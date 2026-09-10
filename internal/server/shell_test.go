package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"math"
	"net"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	gliderlabs "github.com/gliderlabs/ssh"
	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// startShellServer starts a Server on a free port whose only authorized key is
// the returned signer's, and waits until it is accepting connections.
func startShellServer(t *testing.T) (string, ssh.Signer) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	keyring := sshagent.NewKeyring()
	if err := keyring.Add(sshagent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ready := make(chan struct{})
	srv := &Server{
		ListenAddr: addr,
		AuthKeys:   &AgentAuth{Agent: keyring},
		Ready:      func() { close(ready) },
	}
	go func() { _ = srv.ListenAndServe() }()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("server never became ready")
	}
	return addr, signer
}

// dialShellServer connects to addr as signer.
func dialShellServer(t *testing.T, addr string, signer ssh.Signer) *ssh.Client {
	t.Helper()
	conn, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            "test",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

// startPtyShell opens a session, requests a pty of the given size and starts the
// shell, returning what is needed to drive it and to read what it says back.
func startPtyShell(t *testing.T, conn *ssh.Client, rows, cols int) (*ssh.Session, io.WriteCloser, *shellOutput) {
	t.Helper()
	// A hermetic shell: the process running the tests could have inherited any
	// $SHELL, including one with a slow or interactive rc file.
	t.Setenv("SHELL", "/bin/sh")

	sess, err := conn.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := sess.RequestPty("xterm", rows, cols, ssh.TerminalModes{}); err != nil {
		t.Fatalf("request pty: %v", err)
	}
	if err := sess.Shell(); err != nil {
		t.Fatalf("shell: %v", err)
	}
	return sess, stdin, watchShellOutput(stdout)
}

// shellOutput accumulates everything a session prints. One goroutine owns the
// stream for the life of the session, so successive waits carry on where the
// last one stopped instead of competing for the same bytes.
type shellOutput struct {
	chunks chan []byte
	seen   strings.Builder
}

func watchShellOutput(r io.Reader) *shellOutput {
	out := &shellOutput{chunks: make(chan []byte, 64)}
	go func() {
		defer close(out.chunks)
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				out.chunks <- chunk
			}
			if err != nil {
				return
			}
		}
	}()
	return out
}

// wait reads until match is satisfied, failing on timeout with everything seen so
// far so a broken session reports what it did produce rather than hanging.
func (o *shellOutput) wait(t *testing.T, describe string, match func(string) bool, timeout time.Duration) string {
	t.Helper()
	if match(o.seen.String()) {
		return o.seen.String()
	}
	deadline := time.After(timeout)
	for {
		select {
		case chunk, ok := <-o.chunks:
			if !ok {
				t.Fatalf("session output ended before %s appeared; got:\n%s", describe, o.seen.String())
			}
			o.seen.Write(chunk)
			if match(o.seen.String()) {
				return o.seen.String()
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s; got:\n%s", describe, o.seen.String())
		}
	}
}

// waitForText waits for want to appear in the session's output.
func (o *shellOutput) waitForText(t *testing.T, want string, timeout time.Duration) string {
	t.Helper()
	return o.wait(t, strconv.Quote(want), func(seen string) bool {
		return strings.Contains(seen, want)
	}, timeout)
}

// waitForMatch waits for re to match, returning its submatches.
func (o *shellOutput) waitForMatch(t *testing.T, re *regexp.Regexp, timeout time.Duration) []string {
	t.Helper()
	seen := o.wait(t, re.String(), func(seen string) bool {
		return re.MatchString(seen)
	}, timeout)
	return re.FindStringSubmatch(seen)
}

func TestServer_PtySessionRunsAnInteractiveShell(t *testing.T) {
	addr, signer := startShellServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()

	sess, stdin, out := startPtyShell(t, conn, 24, 80)
	defer sess.Close()

	// The pty echoes what is typed, so the marker has to be something the shell
	// must evaluate rather than a literal that would match the echoed line too.
	if _, err := io.WriteString(stdin, "echo sshush-pty-$((6*7))\n"); err != nil {
		t.Fatalf("write to shell: %v", err)
	}
	out.waitForText(t, "sshush-pty-42", 10*time.Second)
}

func TestServer_PtySessionStartsAtTheRequestedSize(t *testing.T) {
	addr, signer := startShellServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()

	sess, stdin, out := startPtyShell(t, conn, 30, 120)
	defer sess.Close()

	if _, err := io.WriteString(stdin, "stty size\n"); err != nil {
		t.Fatalf("write to shell: %v", err)
	}
	out.waitForMatch(t, regexp.MustCompile(`(?m)^30 120\r?$`), 10*time.Second)
}

// Resizing an established session is covered end to end
// (TestE2E_ServerPtyShellResizes) rather than here: gliderlabs writes the new
// window into the Pty struct that every handler reads at session start, which the
// race detector flags on its own account. Out of process, the server is not
// instrumented and the resize path can still be exercised.

func TestServer_PtySessionReportsTheShellExitCode(t *testing.T) {
	addr, signer := startShellServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()

	sess, stdin, _ := startPtyShell(t, conn, 24, 80)
	defer sess.Close()

	if _, err := io.WriteString(stdin, "exit 7\n"); err != nil {
		t.Fatalf("write to shell: %v", err)
	}

	err := waitForSession(t, sess)
	var exitErr *ssh.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("session error = %v, want an *ssh.ExitError", err)
	}
	if exitErr.ExitStatus() != 7 {
		t.Errorf("exit status = %d, want 7", exitErr.ExitStatus())
	}
}

func TestServer_PtySessionEndsCleanlyWhenTheShellExits(t *testing.T) {
	addr, signer := startShellServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()

	sess, stdin, _ := startPtyShell(t, conn, 24, 80)
	defer sess.Close()

	if _, err := io.WriteString(stdin, "exit\n"); err != nil {
		t.Fatalf("write to shell: %v", err)
	}
	if err := waitForSession(t, sess); err != nil {
		t.Errorf("session ended with %v, want a clean exit", err)
	}
}

// waitForSession waits for the remote shell to finish, failing rather than
// blocking the suite if the session never ends.
func waitForSession(t *testing.T, sess *ssh.Session) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("session did not end after the shell exited")
		return nil
	}
}

func TestServer_DisconnectLeavesNoShellBehind(t *testing.T) {
	addr, signer := startShellServer(t)
	conn := dialShellServer(t, addr, signer)

	sess, stdin, out := startPtyShell(t, conn, 24, 80)

	// Ask for the shell's own pid and a background child's, so this covers the
	// whole process group and not just the shell. printf keeps the digits out of
	// the echoed command line, where they would match the pattern spuriously.
	if _, err := io.WriteString(stdin, "sleep 300 & printf 'pids %d %d\\n' $$ $!\n"); err != nil {
		t.Fatalf("write to shell: %v", err)
	}
	pids := out.waitForMatch(t, regexp.MustCompile(`pids (\d+) (\d+)`), 10*time.Second)
	shellPid, err := strconv.Atoi(pids[1])
	if err != nil {
		t.Fatalf("shell pid %q: %v", pids[1], err)
	}
	childPid, err := strconv.Atoi(pids[2])
	if err != nil {
		t.Fatalf("child pid %q: %v", pids[2], err)
	}

	sess.Close()
	conn.Close()

	waitForExit(t, shellPid, "shell")
	waitForExit(t, childPid, "background child")
}

// waitForExit polls until pid is gone, which is how "no orphaned shell process"
// is checked.
func waitForExit(t *testing.T, pid int, what string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("%s (pid %d) still running after the client disconnected", what, pid)
}

// runCommand runs command on a fresh session without a pty, feeding it stdin, and
// returns what it wrote to stdout and stderr and how the session ended. It fails
// rather than blocking the suite if the session never ends.
func runCommand(t *testing.T, conn *ssh.Client, stdin io.Reader, command string) (string, string, error) {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")

	sess, err := conn.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer sess.Close()

	var stdout, stderr strings.Builder
	sess.Stdin = stdin
	sess.Stdout = &stdout
	sess.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- sess.Run(command) }()
	select {
	case err := <-done:
		return stdout.String(), stderr.String(), err
	case <-time.After(10 * time.Second):
		t.Fatalf("remote command %q did not finish", command)
		return "", "", nil
	}
}

func TestServer_RemoteCommandReturnsItsOutput(t *testing.T) {
	addr, signer := startShellServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()

	stdout, stderr, err := runCommand(t, conn, nil, "echo hi")
	if err != nil {
		t.Fatalf("echo hi: %v (stderr %q)", err, stderr)
	}
	if stdout != "hi\n" {
		t.Errorf("stdout = %q, want %q", stdout, "hi\n")
	}
}

func TestServer_RemoteCommandReportsItsOwnExitCode(t *testing.T) {
	addr, signer := startShellServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()

	_, _, err := runCommand(t, conn, nil, "exit 3")
	var exitErr *ssh.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("session error = %v, want an *ssh.ExitError", err)
	}
	if exitErr.ExitStatus() != 3 {
		t.Errorf("exit status = %d, want 3", exitErr.ExitStatus())
	}
}

// TestServer_RemoteCommandRunsThroughTheShell checks the command is handed to the
// shell rather than exec'd as argv, which is what makes pipes and quoting work.
func TestServer_RemoteCommandRunsThroughTheShell(t *testing.T) {
	addr, signer := startShellServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()

	stdout, stderr, err := runCommand(t, conn, nil, "printf '%s\\n' a b c | wc -l")
	if err != nil {
		t.Fatalf("pipeline: %v (stderr %q)", err, stderr)
	}
	if got := strings.TrimSpace(stdout); got != "3" {
		t.Errorf("stdout = %q, want 3", stdout)
	}
}

func TestServer_RemoteCommandKeepsStdoutAndStderrApart(t *testing.T) {
	addr, signer := startShellServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()

	stdout, stderr, err := runCommand(t, conn, nil, "echo out; echo err >&2")
	if err != nil {
		t.Fatalf("command: %v", err)
	}
	if stdout != "out\n" {
		t.Errorf("stdout = %q, want %q", stdout, "out\n")
	}
	if stderr != "err\n" {
		t.Errorf("stderr = %q, want %q", stderr, "err\n")
	}
}

func TestServer_RemoteCommandReadsTheClientsStdin(t *testing.T) {
	addr, signer := startShellServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()

	stdout, stderr, err := runCommand(t, conn, strings.NewReader("from the client\n"), "cat")
	if err != nil {
		t.Fatalf("cat: %v (stderr %q)", err, stderr)
	}
	if stdout != "from the client\n" {
		t.Errorf("stdout = %q, want the client's input back", stdout)
	}
}

// TestServer_RemoteCommandFinishesWhileTheClientsStdinIsOpen covers `ssh host
// echo hi` typed at a terminal, whose stdin never reaches EOF: the session has to
// end when the command does.
func TestServer_RemoteCommandFinishesWhileTheClientsStdinIsOpen(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	addr, signer := startShellServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()

	sess, err := conn.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer sess.Close()
	stdin, err := sess.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	defer stdin.Close()

	if err := sess.Start("echo hi"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := waitForSession(t, sess); err != nil {
		t.Errorf("session ended with %v, want a clean exit", err)
	}
}

func TestServer_ShellWithoutAPtyReadsCommandsFromStdin(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	addr, signer := startShellServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()

	sess, err := conn.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer sess.Close()
	var stdout strings.Builder
	sess.Stdin = strings.NewReader("echo sshush-$((6*7))\n")
	sess.Stdout = &stdout

	if err := sess.Shell(); err != nil {
		t.Fatalf("shell: %v", err)
	}
	if err := waitForSession(t, sess); err != nil {
		t.Fatalf("session ended with %v, want a clean exit", err)
	}
	if stdout.String() != "sshush-42\n" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "sshush-42\n")
	}
}

// TestServer_RemoteCommandCanAskForAPty covers `ssh -t host <command>`, which
// wants the command run on a terminal rather than the interactive shell.
func TestServer_RemoteCommandCanAskForAPty(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	addr, signer := startShellServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()

	sess, err := conn.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer sess.Close()
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := sess.RequestPty("xterm", 30, 120, ssh.TerminalModes{}); err != nil {
		t.Fatalf("request pty: %v", err)
	}
	if err := sess.Start("stty size"); err != nil {
		t.Fatalf("start: %v", err)
	}
	watchShellOutput(stdout).waitForMatch(t, regexp.MustCompile(`(?m)^30 120\r?$`), 10*time.Second)
}

func TestServer_DisconnectEndsARemoteCommand(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	addr, signer := startShellServer(t)
	conn := dialShellServer(t, addr, signer)

	sess, err := conn.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := sess.Start("sleep 300 & printf 'pids %d %d\\n' $$ $!; wait"); err != nil {
		t.Fatalf("start: %v", err)
	}
	pids := watchShellOutput(stdout).waitForMatch(t, regexp.MustCompile(`pids (\d+) (\d+)`), 10*time.Second)
	shellPid, err := strconv.Atoi(pids[1])
	if err != nil {
		t.Fatalf("shell pid %q: %v", pids[1], err)
	}
	childPid, err := strconv.Atoi(pids[2])
	if err != nil {
		t.Fatalf("child pid %q: %v", pids[2], err)
	}

	sess.Close()
	conn.Close()

	waitForExit(t, shellPid, "remote command's shell")
	waitForExit(t, childPid, "remote command's background child")
}

func TestSessionCommand_RunsACommandThroughTheShell(t *testing.T) {
	if got := sessionCommand("/bin/sh", "").Args; len(got) != 1 || got[0] != "/bin/sh" {
		t.Errorf("no command: args = %q, want just the shell", got)
	}
	got := sessionCommand("/bin/sh", "ls | wc -l").Args
	if want := []string{"/bin/sh", "-c", "ls | wc -l"}; strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("with a command: args = %q, want %q", got, want)
	}
}

func TestLoginShell_PrefersTheInheritedShell(t *testing.T) {
	t.Setenv("SHELL", "/usr/local/bin/fish")
	if got := loginShell(); got != "/usr/local/bin/fish" {
		t.Errorf("loginShell() = %q, want /usr/local/bin/fish", got)
	}
}

func TestLoginShell_FallsBackWhenNoShellIsSet(t *testing.T) {
	t.Setenv("SHELL", "   ")
	got := loginShell()
	if got != "/bin/bash" && got != "/bin/sh" {
		t.Errorf("loginShell() = %q, want /bin/bash or /bin/sh", got)
	}
}

func TestWinsize_SubstitutesDefaultsForAZeroWindow(t *testing.T) {
	got := winsize(gliderlabs.Window{})
	if got.Cols != defaultTermCols || got.Rows != defaultTermRows {
		t.Errorf("winsize(0x0) = %dx%d, want %dx%d", got.Cols, got.Rows, defaultTermCols, defaultTermRows)
	}
	got = winsize(gliderlabs.Window{Width: 120, Height: 30})
	if got.Cols != 120 || got.Rows != 30 {
		t.Errorf("winsize(120x30) = %dx%d, want 120x30", got.Cols, got.Rows)
	}
}

func TestWinsize_ClampsAnOversizedWindow(t *testing.T) {
	got := winsize(gliderlabs.Window{Width: 1 << 20, Height: 1 << 20})
	if got.Cols != math.MaxUint16 || got.Rows != math.MaxUint16 {
		t.Errorf("winsize(2^20 square) = %dx%d, want %d square", got.Cols, got.Rows, math.MaxUint16)
	}
}
