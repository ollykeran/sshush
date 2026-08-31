package vault

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/keys"
)

func unixSocketTempDirSessionOps(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "sshush-vault-ops-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// startVaultAgentSocket starts an unlocked vault agent on a temp socket and
// returns the socket path alongside the store behind it.
func startVaultAgentSocket(t *testing.T, dir string, pass []byte) (socketPath string, store *VaultStore) {
	t.Helper()
	socketPath = filepath.Join(dir, "agent.sock")
	store, err := Open(filepath.Join(dir, "vault.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Init(store, pass); err != nil {
		t.Fatal(err)
	}
	va := NewVaultAgent(store)
	if err := va.Unlock(pass); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
		_ = os.Remove(socketPath)
	})
	ready := make(chan struct{})
	go func() {
		_ = agent.ListenAndServe(ctx, socketPath, va, agent.WithReady(func() { close(ready) }))
	}()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("vault agent did not become ready")
	}
	return socketPath, store
}

// writeTestKey writes a freshly generated ed25519 private key and returns its path.
func writeTestKey(t *testing.T, dir, comment string) string {
	t.Helper()
	privPEM, _, err := keys.Generate("ed25519", 0, comment)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(path, privPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func openTestSession(t *testing.T, socketPath string) *agent.Session {
	t.Helper()
	s, err := agent.Open(socketPath)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAddPrivateKeyFile_autoloadPersistsAfterNewAgent(t *testing.T) {
	dir := unixSocketTempDirSessionOps(t)
	keyPath := writeTestKey(t, dir, "persist-me")
	pass := []byte("addpath-test")
	socketPath, store := startVaultAgentSocket(t, dir, pass)

	s := openTestSession(t, socketPath)
	if err := AddPrivateKeyFile(s, keyPath, true); err != nil {
		t.Fatalf("add private key file autoload=true: %v", err)
	}

	va2 := NewVaultAgent(store)
	if err := va2.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	after, err := va2.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].Comment != "persist-me" {
		t.Fatalf("new agent list: want 1 key persist-me, got %d keys", len(after))
	}
}

func TestAddPrivateKeyFile_noAutoloadNotListedAfterNewAgent(t *testing.T) {
	dir := unixSocketTempDirSessionOps(t)
	keyPath := writeTestKey(t, dir, "session-only")
	pass := []byte("addpath-test2")
	socketPath, store := startVaultAgentSocket(t, dir, pass)

	s := openTestSession(t, socketPath)
	if err := AddPrivateKeyFile(s, keyPath, false); err != nil {
		t.Fatalf("add private key file autoload=false: %v", err)
	}

	va2 := NewVaultAgent(store)
	if err := va2.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	after, err := va2.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Fatalf("new agent list: want 0 keys (session-only), got %d", len(after))
	}
}

// TestSessionBackend_realVaultAgent checks agent.Session.Backend against a real
// VaultAgent. internal/agent covers the same contract with a wire-level fake
// because it cannot import this package; this is where the two are reconciled.
func TestSessionBackend_realVaultAgent(t *testing.T) {
	dir := unixSocketTempDirSessionOps(t)
	socketPath, _ := startVaultAgentSocket(t, dir, []byte("backend-probe"))
	s := openTestSession(t, socketPath)

	b, err := s.Backend()
	if err != nil {
		t.Fatalf("backend while unlocked: %v", err)
	}
	if b.Mode != "vault" {
		t.Errorf("mode: want %q, got %q", "vault", b.Mode)
	}
	if b.VaultLocked {
		t.Error("vault locked: want false while unlocked, got true")
	}

	if err := s.Lock(nil); err != nil {
		t.Fatalf("lock: %v", err)
	}
	b, err = s.Backend()
	if err != nil {
		t.Fatalf("backend while locked: %v", err)
	}
	if b.Mode != "vault" {
		t.Errorf("mode: want %q, got %q", "vault", b.Mode)
	}
	if !b.VaultLocked {
		t.Error("vault locked: want true after Lock, got false")
	}
}
