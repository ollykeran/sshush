package sshushd

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func writeTestKey(t *testing.T, dir, filename, comment string) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		t.Fatal(err)
	}
	privPEM := pem.EncodeToMemory(block)
	privPath := filepath.Join(dir, filename)
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return privPath
}

func unixSocketTempDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "sshushd-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestRunAgent_withKeys(t *testing.T) {
	dir := unixSocketTempDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	keyPath := writeTestKey(t, dir, "test_key", "test-key")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- RunAgent(ctx, socketPath, []string{keyPath}, "")
	}()

	if !WaitForSocket(socketPath, 50, 20*time.Millisecond) {
		t.Fatal("agent socket did not become ready")
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial agent socket: %v", err)
	}
	defer conn.Close()

	client := sshagent.NewClient(conn)
	keys, err := client.List()
	if err != nil {
		t.Fatalf("agent list: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].Comment != "test-key" {
		t.Errorf("expected comment 'test-key', got %q", keys[0].Comment)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && !strings.Contains(err.Error(), "closed network connection") {
			t.Fatalf("RunAgent returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunAgent did not exit after context cancel")
	}
}

func TestRunAgent_contextCancel(t *testing.T) {
	dir := unixSocketTempDir(t)
	socketPath := filepath.Join(dir, "agent.sock")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunAgent(ctx, socketPath, nil, "")
	}()

	if !WaitForSocket(socketPath, 50, 20*time.Millisecond) {
		t.Fatal("agent socket did not become ready")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && !strings.Contains(err.Error(), "closed network connection") {
			t.Fatalf("RunAgent returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunAgent did not exit after context cancel")
	}
}

func TestRunAgent_alreadyRunning(t *testing.T) {
	dir := unixSocketTempDir(t)
	socketPath := filepath.Join(dir, "agent.sock")

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = RunAgent(ctx, socketPath, nil, "")
	if err == nil {
		t.Fatal("expected error when socket already in use")
	}
}

func TestRunAgent_vaultPathMissing(t *testing.T) {
	dir := unixSocketTempDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	vaultPath := filepath.Join(dir, "nonexistent.vault")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- RunAgent(ctx, socketPath, nil, vaultPath)
	}()

	if !WaitForSocket(socketPath, 50, 20*time.Millisecond) {
		t.Fatal("agent socket did not become ready")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && !strings.Contains(err.Error(), "closed network connection") {
			t.Fatalf("RunAgent returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunAgent did not exit after context cancel")
	}
}

func TestWaitForSocket_success(t *testing.T) {
	dir := unixSocketTempDir(t)
	socketPath := filepath.Join(dir, "agent.sock")

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ok := WaitForSocket(socketPath, 5, 10*time.Millisecond)
	if !ok {
		t.Fatal("WaitForSocket should return true for existing listener")
	}
}

func TestWaitForSocket_timeout(t *testing.T) {
	dir := unixSocketTempDir(t)
	socketPath := filepath.Join(dir, "nonexistent.sock")

	ok := WaitForSocket(socketPath, 3, 10*time.Millisecond)
	if ok {
		t.Fatal("WaitForSocket should return false for non-existent socket")
	}
}
