package server

import (
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
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

// handleSession runs an interactive shell on a pty for the duration of the SSH session.
// Only PTY sessions are served: a client asking for anything else (a remote command,
// scp, a subsystem) is rejected rather than silently given a shell.
func (s *Server) handleSession(sess gliderlabs.Session) {
	ptyReq, winCh, isPty := sess.Pty()
	if !isPty {
		_, _ = io.WriteString(sess.Stderr(), "sshush: only interactive PTY sessions are supported\n")
		_ = sess.Exit(1)
		return
	}

	cmd := exec.Command(loginShell())
	cmd.Env = append(os.Environ(), "TERM="+ptyReq.Term)

	// Size the pty before the shell starts, not on the first resize event, or it
	// runs at the wrong size until the client's terminal happens to change. Every
	// size after this one arrives on winCh.
	ptyFile, err := pty.StartWithSize(cmd, winsize(ptyReq.Window))
	if err != nil {
		_, _ = io.WriteString(sess.Stderr(), fmt.Sprintf("sshush: start shell: %v\n", err))
		_ = sess.Exit(1)
		return
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

	// A client that vanishes must not leave a shell behind. Watching the session
	// context covers the case where the shell never notices the pty is gone.
	go func() {
		defer watchers.Done()
		select {
		case <-sess.Context().Done():
			terminateShell(cmd, reaped)
		case <-reaped:
		}
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

	_ = sess.Exit(exitCode(cmd.ProcessState))
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

// loginShell picks the shell to run: whatever $SHELL the daemon inherited, then
// bash, then sh. There is deliberately no config knob for it yet.
func loginShell() string {
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
