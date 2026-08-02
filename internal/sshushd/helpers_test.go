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
