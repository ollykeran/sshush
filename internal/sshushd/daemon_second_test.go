package sshushd

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ollykeran/sshush/internal/config"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// Environment a re-run of this test binary reads to play the second daemon (see
// TestSecondDaemonProcess): the socket and pidfile to run against, and the file
// to leave RunDaemonOnly's error in.
const (
	secondDaemonSocketEnv  = "SSHUSHD_TEST_SECOND_SOCKET"
	secondDaemonPidFileEnv = "SSHUSHD_TEST_SECOND_PIDFILE"
	secondDaemonResultEnv  = "SSHUSHD_TEST_SECOND_RESULT"
)

// TestSecondDaemonProcess is not a test of its own: it is the body of the
// second daemon in TestRunDaemonOnly_leavesARunningDaemonAlone. RunDaemonOnly
// detaches, which on Unix points this process's stdout and stderr at /dev/null,
// so it needs a process to itself and reports through a file.
func TestSecondDaemonProcess(t *testing.T) {
	socketPath := os.Getenv(secondDaemonSocketEnv)
	if socketPath == "" {
		t.Skip("only runs as the child of TestRunDaemonOnly_leavesARunningDaemonAlone")
	}
	result := "<nil>"
	if err := RunDaemonOnly(config.Config{SocketPath: socketPath}, os.Getenv(secondDaemonPidFileEnv), nil); err != nil {
		result = err.Error()
	}
	if err := os.WriteFile(os.Getenv(secondDaemonResultEnv), []byte(result), 0o600); err != nil {
		os.Exit(3)
	}
}

// A daemon started while another is serving the socket must fail and take
// nothing with it: the pidfile and the socket file are the running daemon's,
// and losing either leaves it unreachable by `sshush stop` or by ssh.
func TestRunDaemonOnly_leavesARunningDaemonAlone(t *testing.T) {
	dir := unixSocketTempDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	pidFile := filepath.Join(dir, "sshush.pid")
	resultFile := filepath.Join(dir, "second.result")
	keyPath := writeTestKey(t, dir, "test_key", "first-daemon")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	firstDone := make(chan error, 1)
	go func() { firstDone <- RunAgent(ctx, socketPath, []string{keyPath}, "") }()
	if !WaitForSocket(socketPath, 50, 20*time.Millisecond) {
		t.Fatal("first agent's socket did not become ready")
	}
	// Not this process's pid, nor one the second daemon could have: any change
	// to the file shows.
	const firstPid = "2147483646\n"
	if err := os.WriteFile(pidFile, []byte(firstPid), 0o644); err != nil {
		t.Fatal(err)
	}

	second := exec.Command(os.Args[0], "-test.run=^TestSecondDaemonProcess$")
	second.Env = append(os.Environ(),
		secondDaemonSocketEnv+"="+socketPath,
		secondDaemonPidFileEnv+"="+pidFile,
		secondDaemonResultEnv+"="+resultFile,
	)
	finished := make(chan error, 1)
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { finished <- second.Wait() }()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("second daemon process: %v", err)
		}
	case <-time.After(30 * time.Second):
		_ = second.Process.Kill()
		t.Fatal("second daemon kept running: it should have been refused the socket")
	}

	result, err := os.ReadFile(resultFile)
	if err != nil {
		t.Fatalf("second daemon left no result: %v", err)
	}
	if !strings.Contains(string(result), "agent already running at") {
		t.Fatalf("second daemon: got %q, want the already-running error", result)
	}
	if got, err := os.ReadFile(pidFile); err != nil || string(got) != firstPid {
		t.Fatalf("pidfile after the second daemon: %q, %v; want %q untouched", got, err, firstPid)
	}
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("socket after the second daemon: %v", err)
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial first agent: %v", err)
	}
	defer conn.Close()
	keys, err := sshagent.NewClient(conn).List()
	if err != nil {
		t.Fatalf("first agent list: %v", err)
	}
	if len(keys) != 1 || keys[0].Comment != "first-daemon" {
		t.Fatalf("first agent no longer answers with its key: %v", keys)
	}

	cancel()
	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("first agent did not exit after context cancel")
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("the agent that did listen should remove its socket on exit, stat: %v", err)
	}
}

// The in-process agent has the same duty: refused the socket, it must not
// delete it from under whoever is listening.
func TestRunAgent_alreadyRunningKeepsSocket(t *testing.T) {
	dir := unixSocketTempDir(t)
	socketPath := filepath.Join(dir, "agent.sock")

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	if err := RunAgent(context.Background(), socketPath, nil, ""); err == nil {
		t.Fatal("expected error when socket already in use")
	}
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("socket was removed by the agent that was refused it: %v", err)
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("listener no longer reachable: %v", err)
	}
	conn.Close()
}
