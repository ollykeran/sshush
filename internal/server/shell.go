package server

import (
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	gliderlabs "github.com/gliderlabs/ssh"
)

const (
	// defaultTermCols and defaultTermRows size the pty when a client asks for
	// one without giving a window size.
	defaultTermCols = 80
	defaultTermRows = 24

	// shellKillGrace is how long a disconnected session's shell has to act on
	// SIGHUP before it is killed outright.
	shellKillGrace = 2 * time.Second
)

// handleSession runs what the client asked for — an interactive shell, or a remote
// command (`ssh host <command>`) — for the duration of the SSH session. It runs on a
// pty when the client requested one (a terminal login, or `ssh -t host <command>`)
// and over plain pipes otherwise. Subsystems such as sftp never reach here.
func (s *Server) handleSession(sess gliderlabs.Session) {
	shell := loginShell(s.shellPath)
	cmd := sessionCommand(shell, sess.RawCommand())
	cmd.Env = sessionEnv(os.Environ(), sess.RemoteAddr(), sess.LocalAddr(), shell)

	var code int
	var err error
	if ptyReq, winCh, isPty := sess.Pty(); isPty {
		code, err = runOnPty(sess, cmd, ptyReq, winCh)
	} else {
		code, err = runOnPipes(sess, cmd)
	}
	if err != nil {
		_, _ = io.WriteString(sess.Stderr(), fmt.Sprintf("sshush: start shell: %v\n", err))
		_ = sess.Exit(1)
		return
	}
	_ = sess.Exit(code)
}

// sessionCommand builds the process a session runs, as sshd does. With no command
// it is the shell as a login shell: argv[0] prefixed with "-", which is what tells
// a shell to read ~/.profile and friends the way a terminal login does. With one it
// is the shell running it via `shell -c`, rather than the command exec'd as argv, so
// `ssh host 'ls | wc -l'` gets the pipes, globbing and quoting whoever typed it was
// counting on.
func sessionCommand(shell, rawCommand string) *exec.Cmd {
	if rawCommand == "" {
		cmd := exec.Command(shell)
		cmd.Args[0] = "-" + filepath.Base(shell)
		return cmd
	}
	return exec.Command(shell, "-c", rawCommand)
}

// runOnPty runs cmd on a pty sized from the client's request, relaying resizes,
// and returns its exit status once it has ended.
func runOnPty(sess gliderlabs.Session, cmd *exec.Cmd, ptyReq gliderlabs.Pty, winCh <-chan gliderlabs.Window) (int, error) {
	ptyFile, tty, err := pty.Open()
	if err != nil {
		return 0, err
	}
	// Size the pty before the shell starts, not on the first resize event, or it
	// runs at the wrong size until the client's terminal happens to change. Every
	// size after this one arrives on winCh.
	if err := pty.Setsize(ptyFile, winsize(ptyReq.Window)); err != nil {
		_ = tty.Close()
		_ = ptyFile.Close()
		return 0, err
	}

	// Opening the pty here rather than through pty.StartWithSize is what lets
	// SSH_TTY name it. The rest is what StartWithSize would do: the shell leads a
	// new session, with the pty as its controlling terminal.
	cmd.Env = append(cmd.Env, "TERM="+ptyReq.Term, "SSH_TTY="+tty.Name())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	err = cmd.Start()
	// The shell has its own descriptors for the tty now. Holding this one open
	// would keep the pty read below from ever seeing the shell hang up.
	_ = tty.Close()
	if err != nil {
		_ = ptyFile.Close()
		return 0, err
	}

	// reaped closes once the shell has been waited on, which is what tells the
	// two goroutines below that the session is over.
	reaped := make(chan struct{})

	var watchers sync.WaitGroup
	watchers.Add(2)
	go func() {
		defer watchers.Done()
		for {
			select {
			case win, ok := <-winCh:
				if !ok {
					return
				}
				_ = pty.Setsize(ptyFile, winsize(win))
			case <-reaped:
				return
			}
		}
	}()

	// Watching the session context covers the case where the shell never notices
	// the pty is gone.
	go func() {
		defer watchers.Done()
		hangUpOnDisconnect(sess, cmd, reaped)
	}()

	// The client-to-shell copy does not end on its own: sess only reaches EOF once
	// the session is already closing. Closing the pty below is what releases it —
	// it cannot be joined, but writing to a closed *os.File is safe, where the
	// ioctl the resize watcher makes is not. Hence the WaitGroup.
	go func() { _, _ = io.Copy(ptyFile, sess) }()

	// Reading the pty is the session's lifeline: it ends when the shell exits and
	// drops the last descriptor on the far side of the terminal.
	_, _ = io.Copy(sess, ptyFile)
	_ = cmd.Wait()
	close(reaped)
	watchers.Wait()
	_ = ptyFile.Close()
	return exitCode(cmd.ProcessState), nil
}

// runOnPipes runs cmd with its stdin, stdout and stderr relayed over the session,
// as a remote command without a pty is, and returns its exit status once it has
// ended.
func runOnPipes(sess gliderlabs.Session, cmd *exec.Cmd) (int, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return 0, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return 0, err
	}
	// A process group of its own, which a pty would otherwise have given it, so a
	// disconnect can signal the command and whatever it started together.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}

	reaped := make(chan struct{})
	var watcher sync.WaitGroup
	watcher.Add(1)
	go func() {
		defer watcher.Done()
		hangUpOnDisconnect(sess, cmd, reaped)
	}()

	// Client input is relayed but never waited on: a command that has finished ends
	// the session whether or not the client closes its stdin, which `ssh host
	// echo hi` typed at a terminal never does.
	go func() {
		_, _ = io.Copy(stdin, sess)
		_ = stdin.Close()
	}()

	// Output is what the session waits on instead. It ends once the command and
	// everything that inherited its output are done, as with sshd. Wait closes the
	// read ends, so it must not run before the copies finish — unless the client
	// has gone, when nobody is left to read them and that close is what releases
	// the copies.
	outputDone := make(chan struct{})
	go func() {
		var copies sync.WaitGroup
		copies.Add(2)
		go func() { defer copies.Done(); _, _ = io.Copy(sess, stdout) }()
		go func() { defer copies.Done(); _, _ = io.Copy(sess.Stderr(), stderr) }()
		copies.Wait()
		close(outputDone)
	}()
	select {
	case <-outputDone:
	case <-sess.Context().Done():
	}

	_ = cmd.Wait()
	close(reaped)
	watcher.Wait()
	return exitCode(cmd.ProcessState), nil
}

// hangUpOnDisconnect terminates cmd if the client goes away before cmd has been
// reaped. A client that vanishes must not leave anything it started behind.
func hangUpOnDisconnect(sess gliderlabs.Session, cmd *exec.Cmd, reaped <-chan struct{}) {
	select {
	case <-sess.Context().Done():
		terminateShell(cmd, reaped)
	case <-reaped:
	}
}

// terminateShell sends SIGHUP to the shell's process group and, if the shell is
// still there after a grace period, SIGKILL. Signalling the group rather than the
// process takes whatever the user started — an editor, a pager — with it; the
// shell leads its own session and process group because the pty put it there.
func terminateShell(cmd *exec.Cmd, reaped <-chan struct{}) {
	if cmd.Process == nil {
		return
	}
	pgid := cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGHUP)
	select {
	case <-reaped:
		return
	case <-time.After(shellKillGrace):
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

// exitCode maps a finished shell to the status reported to the SSH client. A
// process killed by a signal has no exit code of its own, so it is reported the
// way a shell reports a signalled child.
func exitCode(state *os.ProcessState) int {
	if state == nil {
		return 1
	}
	if code := state.ExitCode(); code >= 0 {
		return code
	}
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return 1
}

// winsize converts an SSH window request to pty dimensions, substituting a
// conventional 80x24 for either dimension the client left at zero. Dimensions
// come off the wire, so they are clamped rather than wrapped into uint16.
func winsize(win gliderlabs.Window) *pty.Winsize {
	return &pty.Winsize{
		Rows: dimension(win.Height, defaultTermRows),
		Cols: dimension(win.Width, defaultTermCols),
	}
}

func dimension(requested, fallback int) uint16 {
	switch {
	case requested <= 0:
		return uint16(fallback)
	case requested > math.MaxUint16:
		return math.MaxUint16
	default:
		return uint16(requested)
	}
}

// ResolveShell finds a configured shell the way a command line would — a path as
// given, a bare name on PATH — and returns its path. Checking it before any
// session needs it is what turns a typo into a startup error rather than a
// failure served to every client.
func ResolveShell(name string) (string, error) {
	return exec.LookPath(name)
}

// loginShell picks the shell to run: the configured one if there is one, then
// whatever $SHELL the daemon inherited, then bash, then sh.
func loginShell(configured string) string {
	if configured != "" {
		return configured
	}
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		return shell
	}
	for _, candidate := range []string{"/bin/bash", "/bin/sh"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return "/bin/sh"
}
