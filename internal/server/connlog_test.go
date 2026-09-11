package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// logBuffer collects a server's log for a test to read while the server writes.
type logBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// startLoggedServer starts a shell server logging at debug into the returned buffer.
func startLoggedServer(t *testing.T, configure ...func(*Server)) (string, ssh.Signer, *logBuffer) {
	t.Helper()
	log := &logBuffer{}
	withLog := func(s *Server) { s.Log = slog.New(NewLogHandler(log, slog.LevelDebug)) }
	addr, signer := startShellServer(t, append([]func(*Server){withLog}, configure...)...)
	return addr, signer, log
}

// waitForLog waits for the log to match pattern — lines about a connection's end
// are written as the server notices it, after the client has moved on — and
// fails with the whole log if it never does.
func waitForLog(t *testing.T, log *logBuffer, pattern string) []string {
	t.Helper()
	re := regexp.MustCompile(pattern)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if m := re.FindStringSubmatch(log.String()); m != nil {
			return m
		}
		if time.Now().After(deadline) {
			t.Fatalf("log never matched %s; got:\n%s", pattern, log.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// clientPort is the port conn's client end is bound to, which the server logs it by.
func clientPort(conn *ssh.Client) string {
	return strconv.Itoa(conn.LocalAddr().(*net.TCPAddr).Port)
}

func TestServer_LogsItsStartup(t *testing.T) {
	addr, _, log := startLoggedServer(t)
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}

	waitForLog(t, log, `sshushd\[\d+\]: Server listening on 127\.0\.0\.1 port `+port+`\.`)
	waitForLog(t, log, `Host key: ephemeral, for this run only`)
	waitForLog(t, log, `(?m)Authentication methods offered: publickey$`)
	waitForLog(t, log, `Sessions run \S+`)
}

func TestServer_LogsASignInAndItsSessionLikeSshd(t *testing.T) {
	addr, signer, log := startLoggedServer(t)
	_, serverPort, _ := net.SplitHostPort(addr)
	conn := dialShellServer(t, addr, signer)
	port := clientPort(conn)
	from := `test from 127\.0\.0\.1 port ` + port

	if _, _, err := runCommand(t, conn, nil, "exit 3"); err == nil {
		t.Fatal("exit 3 ended cleanly, want exit status 3")
	}
	conn.Close()

	waitForLog(t, log, `Connection from 127\.0\.0\.1 port `+port+` on 127\.0\.0\.1 port `+serverPort)
	waitForLog(t, log, `Authentication methods offered to `+from+`: publickey`)
	waitForLog(t, log, `Accepted publickey for `+from+` ssh2: ED25519 `+regexp.QuoteMeta(ssh.FingerprintSHA256(signer.PublicKey())))
	waitForLog(t, log, `Starting session: command for `+from)
	waitForLog(t, log, `debug: Session command for `+from+`: exit 3`)
	waitForLog(t, log, `Session closed for `+from+`: exit status 3`)
	waitForLog(t, log, `Disconnected from user test 127\.0\.0\.1 port `+port+` \(connected \S+\)`)
}

func TestServer_LogsARefusedKey(t *testing.T) {
	addr, _, log := startLoggedServer(t)
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}

	conn, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            "test",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(stranger)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err == nil {
		conn.Close()
		t.Fatal("an unauthorized key signed in")
	}

	waitForLog(t, log, `Failed publickey for test from 127\.0\.0\.1 port \d+ ssh2: ED25519 `+
		regexp.QuoteMeta(ssh.FingerprintSHA256(stranger.PublicKey()))+`; methods offered: publickey`)
	waitForLog(t, log, `Connection closed by authenticating user test 127\.0\.0\.1 port \d+ \[preauth\]`)
}

func TestServer_LogsPasswordAttempts(t *testing.T) {
	addr, _, log := startLoggedServer(t, func(s *Server) {
		s.Passwords = &fakePasswords{accept: "right"}
	})

	if conn, err := dialWithPassword(addr, "wrong"); err == nil {
		conn.Close()
		t.Fatal("a wrong password signed in")
	}
	waitForLog(t, log, `(?m)Failed password for test from 127\.0\.0\.1 port \d+ ssh2; methods offered: publickey,password$`)

	conn, err := dialWithPassword(addr, "right")
	if err != nil {
		t.Fatalf("dial with the right password: %v", err)
	}
	conn.Close()
	waitForLog(t, log, `(?m)Accepted password for test from 127\.0\.0\.1 port \d+ ssh2$`)
}

// TestServer_LogsAPasswordLockoutAfterTheFailureThatCausedIt checks the lockout
// warning follows the failed-password line that tripped it, so "after 5 failed
// passwords" is read after the fifth.
func TestServer_LogsAPasswordLockoutAfterTheFailureThatCausedIt(t *testing.T) {
	log := &logBuffer{}
	s := &Server{Log: slog.New(NewLogHandler(log, slog.LevelInfo)), Passwords: &fakePasswords{}}
	state := &connState{remote: "203.0.113.5 port 40120"}
	state.notePasswordCheck("", "203.0.113.5")

	s.logAuth(state, "root", "password", errors.New("permission denied"))

	failed := strings.Index(log.String(), "Failed password for root from 203.0.113.5 port 40120 ssh2; methods offered: publickey,password")
	warning := strings.Index(log.String(), fmt.Sprintf(
		"warning: Locking 203.0.113.5 out of password authentication for %v after %d failed passwords",
		passwordLockout, passwordLockoutFailures))
	if failed < 0 || warning < 0 || warning < failed {
		t.Errorf("want the failed password, then the lockout warning; got:\n%s", log.String())
	}
}

func TestServer_LogsWhatItRefuses(t *testing.T) {
	addr, signer, log := startLoggedServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()
	from := `test from 127\.0\.0\.1 port ` + clientPort(conn)

	if forwarded, err := conn.Dial("tcp", "127.0.0.1:9"); err == nil {
		forwarded.Close()
	}
	waitForLog(t, log, `Refused port forwarding to 127\.0\.0\.1 port 9 for `+from)

	if listener, err := conn.Listen("tcp", "127.0.0.1:0"); err == nil {
		listener.Close()
	}
	waitForLog(t, log, `Refused remote port forwarding on 127\.0\.0\.1 port 0 for `+from)

	sess, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	_ = sess.RequestSubsystem("sftp")
	waitForLog(t, log, `Refused subsystem request for sftp by user `+from+`: not supported`)

	if ch, requests, err := conn.OpenChannel("sshush-test@example.com", nil); err == nil {
		go ssh.DiscardRequests(requests)
		ch.Close()
	}
	waitForLog(t, log, `Refused sshush-test@example\.com channel for `+from)
}

func TestServer_LogsTheTerminalAPtySessionRunsOn(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	addr, signer, log := startLoggedServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()

	sess, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if err := sess.RequestPty("xterm", 24, 80, ssh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	if err := sess.Run("true"); err != nil {
		t.Fatalf("true on a pty: %v", err)
	}
	waitForLog(t, log, `Starting session: command on (ttys\d+|pts/\d+) for test from 127\.0\.0\.1 port `+clientPort(conn))
}

func TestServer_CloseHangsUpOnLiveSessions(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	var srv *Server
	addr, signer, log := startLoggedServer(t, func(s *Server) { srv = s })
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()

	sess, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Start("sleep 300 & printf 'pids %d %d\\n' $$ $!; wait"); err != nil {
		t.Fatal(err)
	}
	pids := watchShellOutput(stdout).waitForMatch(t, regexp.MustCompile(`pids (\d+) (\d+)`), 10*time.Second)

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for i, what := range []string{"shell", "background child"} {
		pid, _ := strconv.Atoi(pids[i+1])
		waitForExit(t, pid, what)
	}
	waitForLog(t, log, `Session closed for test from 127\.0\.0\.1 port \d+: exit status \d+`)
	if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		c.Close()
		t.Error("the server still accepts connections after Close")
	}
}

func TestServer_ListenAndServeReturnsNilAfterClose(t *testing.T) {
	ready := make(chan struct{})
	srv := &Server{
		ListenAddr: "127.0.0.1:0",
		AuthKeys:   &AgentAuth{Agent: sshagent.NewKeyring()},
		Ready:      func() { close(ready) },
	}
	served := make(chan error, 1)
	go func() { served <- srv.ListenAndServe() }()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("server never became ready")
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Errorf("ListenAndServe after Close = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ListenAndServe kept serving after Close")
	}
}

func TestKeyTypeLabel(t *testing.T) {
	cases := map[string]string{
		"ssh-ed25519":                        "ED25519",
		"ssh-rsa":                            "RSA",
		"ecdsa-sha2-nistp256":                "ECDSA",
		"sk-ssh-ed25519@openssh.com":         "ED25519-SK",
		"sk-ecdsa-sha2-nistp256@openssh.com": "ECDSA-SK",
		"ssh-ed25519-cert-v01@openssh.com":   "ED25519-CERT",
		"something-new":                      "something-new",
	}
	for keyType, want := range cases {
		if got := keyTypeLabel(keyType); got != want {
			t.Errorf("keyTypeLabel(%q) = %q, want %q", keyType, got, want)
		}
	}
}
