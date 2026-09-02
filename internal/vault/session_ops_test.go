package vault

import (
	"context"
	"errors"
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

// startLockedVaultAgentSocket starts a vault agent that is locked, so operations
// fail for a known reason.
func startLockedVaultAgentSocket(t *testing.T, dir string, pass []byte) string {
	t.Helper()
	socketPath, _ := startVaultAgentSocket(t, dir, pass)
	s := openTestSession(t, socketPath)
	if err := s.Lock(nil); err != nil {
		t.Fatalf("lock: %v", err)
	}
	return socketPath
}

// TestOp_lockedVaultReportsReason is the point of the sshush-op extension: the
// legacy extension collapses every failure into one opaque string, while the op
// carries the reason across the wire.
func TestOp_lockedVaultReportsReason(t *testing.T) {
	dir := unixSocketTempDirSessionOps(t)
	socketPath := startLockedVaultAgentSocket(t, dir, []byte("locked-reason"))
	s := openTestSession(t, socketPath)

	// Legacy path: reason destroyed.
	_, legacyErr := s.Extension(ExtensionVaultSessionLoad, []byte("SHA256:whatever"))
	if legacyErr == nil {
		t.Fatal("legacy session-load against a locked vault: want error, got nil")
	}
	if legacyErr.Error() != "agent: generic extension failure" {
		t.Errorf("legacy error: want the opaque protocol string, got %q", legacyErr.Error())
	}

	// Op path: reason survives.
	_, err := s.Op(agent.OpSessionLoad, []byte("SHA256:whatever"))
	if !errors.Is(err, agent.ErrVaultLocked) {
		t.Errorf("op error: want ErrVaultLocked, got %v", err)
	}
}

func TestOp_unknownIdentityReportsNotFound(t *testing.T) {
	dir := unixSocketTempDirSessionOps(t)
	socketPath, _ := startVaultAgentSocket(t, dir, []byte("notfound-reason"))
	s := openTestSession(t, socketPath)

	_, err := s.Op(agent.OpSessionLoad, []byte("SHA256:definitely-not-present"))
	if !errors.Is(err, agent.ErrIdentityNotFound) {
		t.Errorf("op error: want ErrIdentityNotFound, got %v", err)
	}
}

func TestOp_recoveryDistinguishesNoRecoveryFromWrongPhrase(t *testing.T) {
	dir := unixSocketTempDirSessionOps(t)
	socketPath := startLockedVaultAgentSocket(t, dir, []byte("recovery-reason"))
	s := openTestSession(t, socketPath)

	// startVaultAgentSocket calls Init without enabling recovery, so no phrase
	// can ever work. That is a different fact from "wrong phrase".
	_, err := s.Op(agent.OpUnlockRecovery, []byte("abandon abandon abandon"))
	if !errors.Is(err, agent.ErrNoRecovery) {
		t.Errorf("op error: want ErrNoRecovery, got %v", err)
	}
}

func TestOp_unlockReportsWrongPassphraseAndNotLocked(t *testing.T) {
	dir := unixSocketTempDirSessionOps(t)
	pass := []byte("unlock-reason")
	socketPath := startLockedVaultAgentSocket(t, dir, pass)
	s := openTestSession(t, socketPath)

	if _, err := s.Op(agent.OpUnlock, []byte("not-the-passphrase")); !errors.Is(err, agent.ErrWrongPassphrase) {
		t.Errorf("wrong passphrase: want ErrWrongPassphrase, got %v", err)
	}
	if _, err := s.Op(agent.OpUnlock, pass); err != nil {
		t.Fatalf("unlock with the right passphrase: %v", err)
	}
	// Unlocking an unlocked vault is the case cli/unlock.go could never detect.
	if _, err := s.Op(agent.OpUnlock, pass); !errors.Is(err, agent.ErrNotLocked) {
		t.Errorf("already unlocked: want ErrNotLocked, got %v", err)
	}
}

func TestOp_vaultLockedReportsState(t *testing.T) {
	dir := unixSocketTempDirSessionOps(t)
	socketPath, _ := startVaultAgentSocket(t, dir, []byte("state-probe"))
	s := openTestSession(t, socketPath)

	data, err := s.Op(agent.OpVaultLocked, nil)
	if err != nil {
		t.Fatalf("vault-locked op: %v", err)
	}
	if len(data) != 1 || data[0] != 0 {
		t.Fatalf("unlocked state: want [0], got %v", data)
	}
	if _, err := s.Op(agent.OpLock, nil); err != nil {
		t.Fatalf("lock op: %v", err)
	}
	data, err = s.Op(agent.OpVaultLocked, nil)
	if err != nil {
		t.Fatalf("vault-locked op after lock: %v", err)
	}
	if len(data) != 1 || data[0] != 1 {
		t.Errorf("locked state: want [1], got %v", data)
	}
}

func TestOp_setAutoloadAndCommentRoundTrip(t *testing.T) {
	dir := unixSocketTempDirSessionOps(t)
	keyPath := writeTestKey(t, dir, "op-roundtrip")
	socketPath, store := startVaultAgentSocket(t, dir, []byte("roundtrip"))
	s := openTestSession(t, socketPath)

	if _, err := s.Op(agent.OpAddKey, mustAddKeyPayload(t, keyPath, true)); err != nil {
		t.Fatalf("add key op: %v", err)
	}
	ids, err := ListIdentities(store)
	if err != nil || len(ids) != 1 {
		t.Fatalf("want 1 identity, got %d (err %v)", len(ids), err)
	}
	fp := ids[0].Fingerprint

	if _, err := s.Op(agent.OpSetComment, BuildSetCommentPayload(fp, "renamed")); err != nil {
		t.Fatalf("set comment op: %v", err)
	}
	if _, err := s.Op(agent.OpSetAutoload, BuildSetAutoloadPayload(fp, false)); err != nil {
		t.Fatalf("set autoload op: %v", err)
	}

	ids, err = ListIdentities(store)
	if err != nil {
		t.Fatal(err)
	}
	if ids[0].Comment != "renamed" {
		t.Errorf("comment: want %q, got %q", "renamed", ids[0].Comment)
	}
	if ids[0].Autoload {
		t.Error("autoload: want false after set-autoload off, got true")
	}
}

func mustAddKeyPayload(t *testing.T, path string, autoload bool) []byte {
	t.Helper()
	payload, err := BuildAddKeyOptsPayload(path, autoload)
	if err != nil {
		t.Fatalf("build add-key payload: %v", err)
	}
	return payload
}
