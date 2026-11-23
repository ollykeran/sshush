package sshushd

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindBinary_notFound(t *testing.T) {
	t.Setenv("PATH", "")

	bin, err := FindBinary()
	if err == nil {
		t.Fatalf("expected error, got binary %q", bin)
	}
	if !strings.Contains(err.Error(), "binary not found") {
		t.Errorf("expected 'binary not found' in error, got %q", err.Error())
	}
}

func TestStopDaemon_missingPidfile(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "nonexistent.pid")

	err := StopDaemon(pidPath)
	if err == nil {
		t.Fatal("expected error for missing pidfile")
	}
	if !strings.Contains(err.Error(), "no pidfile") {
		t.Errorf("expected 'no pidfile' in error, got %q", err.Error())
	}
}

func TestStopDaemon_invalidPidfile(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "bad.pid")

	if err := os.WriteFile(pidPath, []byte("not-a-number\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := StopDaemon(pidPath)
	if err == nil {
		t.Fatal("expected error for invalid pidfile")
	}
	if !strings.Contains(err.Error(), "invalid pidfile") {
		t.Errorf("expected 'invalid pidfile' in error, got %q", err.Error())
	}
}

func TestCheckAlreadyRunning_true(t *testing.T) {
	dir := unixSocketTempDir(t)
	socketPath := filepath.Join(dir, "test.sock")

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	if !CheckAlreadyRunning(socketPath) {
		t.Fatal("CheckAlreadyRunning should return true for existing listener")
	}
}

func TestCheckAlreadyRunning_false(t *testing.T) {
	dir := unixSocketTempDir(t)
	socketPath := filepath.Join(dir, "nonexistent.sock")

	if CheckAlreadyRunning(socketPath) {
		t.Fatal("CheckAlreadyRunning should return false for non-existent socket")
	}
}

func TestPidFileLive(t *testing.T) {
	dir := t.TempDir()

	missing := filepath.Join(dir, "missing.pid")
	if PidFileLive(missing) {
		t.Error("a missing pidfile must not count as a running daemon")
	}

	// Unreadable as a pid: refuse to guess, so a second daemon is not started.
	garbled := filepath.Join(dir, "garbled.pid")
	if err := os.WriteFile(garbled, []byte("not-a-number\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !PidFileLive(garbled) {
		t.Error("an unparsable pidfile must count as live")
	}

	// A pid no process has: what a daemon that was killed leaves behind.
	stale := filepath.Join(dir, "stale.pid")
	if err := os.WriteFile(stale, []byte("2147483646\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if PidFileLive(stale) {
		t.Error("a pidfile naming a dead process must not count as a running daemon")
	}
}
