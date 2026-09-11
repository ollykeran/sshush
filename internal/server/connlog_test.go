package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
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

// records decodes the records logged so far. Tests log as JSON and read the
// records back, so they check what was recorded rather than how a handler lays
// it out.
func (l *logBuffer) records(t *testing.T) []map[string]any {
	t.Helper()
	var recs []map[string]any
	for _, line := range strings.Split(l.String(), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line %q is not a JSON record: %v", line, err)
		}
		recs = append(recs, rec)
	}
	return recs
}

func jsonLogger(log *logBuffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// startLoggedServer starts a shell server logging at debug into the returned buffer.
func startLoggedServer(t *testing.T, configure ...func(*Server)) (string, ssh.Signer, *logBuffer) {
	t.Helper()
	log := &logBuffer{}
	withLog := func(s *Server) { s.Log = jsonLogger(log) }
	addr, signer := startShellServer(t, append([]func(*Server){withLog}, configure...)...)
	return addr, signer, log
}

// recordMatches reports whether rec has message msg and every field in fields.
// Values are compared as printed; a *regexp.Regexp matches the printed value instead.
func recordMatches(rec map[string]any, msg string, fields map[string]any) bool {
	if rec["msg"] != msg {
		return false
	}
	for key, want := range fields {
		got, ok := rec[key]
		if !ok {
			return false
		}
		if re, isRegexp := want.(*regexp.Regexp); isRegexp {
			if !re.MatchString(fmt.Sprint(got)) {
				return false
			}
		} else if fmt.Sprint(got) != fmt.Sprint(want) {
			return false
		}
	}
	return true
}

// recordIndex is the position of the first record matching msg and fields, or -1.
func recordIndex(recs []map[string]any, msg string, fields map[string]any) int {
	for i, rec := range recs {
		if recordMatches(rec, msg, fields) {
			return i
		}
	}
	return -1
}

// waitForRecord waits for a record matching msg and fields — records about a
// connection's end are logged as the server notices, after the client has moved
// on — and fails with the whole log if none turns up.
func waitForRecord(t *testing.T, log *logBuffer, msg string, fields map[string]any) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for recordIndex(log.records(t), msg, fields) < 0 {
		if time.Now().After(deadline) {
			t.Fatalf("no %q record with %v in the log:\n%s", msg, fields, log.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServer_LogsItsStartup(t *testing.T) {
	addr, _, log := startLoggedServer(t)
	waitForRecord(t, log, "server listening", map[string]any{
		"addr":         addr,
		"host_key":     "ephemeral",
		"auth_methods": "publickey",
		"shell":        regexp.MustCompile(`\S`),
	})
}

func TestServer_LogsASignInAndItsSession(t *testing.T) {
	addr, signer, log := startLoggedServer(t)
	conn := dialShellServer(t, addr, signer)
	remote := conn.LocalAddr().String()

	if _, _, err := runCommand(t, conn, nil, "exit 3"); err == nil {
		t.Fatal("exit 3 ended cleanly, want exit status 3")
	}
	conn.Close()

	waitForRecord(t, log, "connection opened", map[string]any{"remote": remote, "local": addr})
	waitForRecord(t, log, "auth methods offered", map[string]any{"user": "test", "remote": remote, "methods": "publickey"})
	waitForRecord(t, log, "auth accepted", map[string]any{
		"method":      "publickey",
		"user":        "test",
		"remote":      remote,
		"key_type":    "ssh-ed25519",
		"fingerprint": ssh.FingerprintSHA256(signer.PublicKey()),
	})
	waitForRecord(t, log, "session started", map[string]any{"kind": "command", "user": "test", "remote": remote})
	waitForRecord(t, log, "session details", map[string]any{"level": "DEBUG", "remote": remote, "command": "exit 3"})
	waitForRecord(t, log, "session closed", map[string]any{"user": "test", "remote": remote, "exit_status": 3})
	waitForRecord(t, log, "connection closed", map[string]any{"user": "test", "remote": remote, "authenticated": true})
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

	waitForRecord(t, log, "auth failed", map[string]any{
		"method":          "publickey",
		"user":            "test",
		"fingerprint":     ssh.FingerprintSHA256(stranger.PublicKey()),
		"methods_offered": "publickey",
	})
	waitForRecord(t, log, "connection closed", map[string]any{"user": "test", "authenticated": false})
}

func TestServer_LogsPasswordAttempts(t *testing.T) {
	addr, _, log := startLoggedServer(t, func(s *Server) {
		s.Passwords = &fakePasswords{accept: "right"}
	})

	if conn, err := dialWithPassword(addr, "wrong"); err == nil {
		conn.Close()
		t.Fatal("a wrong password signed in")
	}
	waitForRecord(t, log, "auth failed", map[string]any{
		"method":          "password",
		"user":            "test",
		"methods_offered": "publickey,password",
	})

	conn, err := dialWithPassword(addr, "right")
	if err != nil {
		t.Fatalf("dial with the right password: %v", err)
	}
	conn.Close()
	waitForRecord(t, log, "auth accepted", map[string]any{"method": "password", "user": "test"})
}

// TestServer_LogsAPasswordLockoutAfterTheFailureThatCausedIt checks the lockout
// warning follows the failed attempt that tripped it.
func TestServer_LogsAPasswordLockoutAfterTheFailureThatCausedIt(t *testing.T) {
	log := &logBuffer{}
	s := &Server{Log: jsonLogger(log), Passwords: &fakePasswords{}}
	state := &connState{remote: "203.0.113.5:40120"}
	state.notePasswordCheck("", "203.0.113.5")

	s.logAuth(state, "root", "password", errors.New("permission denied"))

	recs := log.records(t)
	failed := recordIndex(recs, "auth failed", map[string]any{"method": "password", "user": "root", "remote": "203.0.113.5:40120"})
	lockout := recordIndex(recs, "password lockout", map[string]any{
		"level":    "WARN",
		"host":     "203.0.113.5",
		"failures": passwordLockoutFailures,
	})
	if failed < 0 || lockout < 0 || lockout < failed {
		t.Errorf("want the failed attempt, then the lockout warning; got:\n%s", log.String())
	}
}

func TestServer_LogsWhatItRefuses(t *testing.T) {
	addr, signer, log := startLoggedServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()
	who := func(fields map[string]any) map[string]any {
		fields["user"] = "test"
		fields["remote"] = conn.LocalAddr().String()
		return fields
	}

	if forwarded, err := conn.Dial("tcp", "127.0.0.1:9"); err == nil {
		forwarded.Close()
	}
	waitForRecord(t, log, "port forwarding refused", who(map[string]any{"destination": "127.0.0.1:9"}))

	if listener, err := conn.Listen("tcp", "127.0.0.1:0"); err == nil {
		listener.Close()
	}
	waitForRecord(t, log, "remote port forwarding refused", who(map[string]any{"bind": "127.0.0.1:0"}))

	sess, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	_ = sess.RequestSubsystem("sftp")
	waitForRecord(t, log, "subsystem refused", who(map[string]any{"subsystem": "sftp"}))

	if ch, requests, err := conn.OpenChannel("sshush-test@example.com", nil); err == nil {
		go ssh.DiscardRequests(requests)
		ch.Close()
	}
	waitForRecord(t, log, "channel refused", who(map[string]any{"type": "sshush-test@example.com"}))
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
	waitForRecord(t, log, "session started", map[string]any{
		"kind":   "command",
		"remote": conn.LocalAddr().String(),
		"tty":    regexp.MustCompile(`^/dev/(ttys\d+|pts/\d+)$`),
	})
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
	waitForRecord(t, log, "session closed", map[string]any{"user": "test", "remote": conn.LocalAddr().String()})
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
