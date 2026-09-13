package editcomment

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	sshagent "golang.org/x/crypto/ssh/agent"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/vault"
	ssh "golang.org/x/crypto/ssh"
)

// unixSocketTempDir returns a short-path temp dir suitable for a unix socket,
// since t.TempDir()'s nested path can exceed the platform's socket path limit.
func unixSocketTempDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "sshush-editcomment-a-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// startTestKeyringAgent starts an in-process keys-mode SSH agent on a temp unix socket.
func startTestKeyringAgent(t *testing.T) (socketPath string, client sshagent.ExtendedAgent) {
	t.Helper()
	dir := unixSocketTempDir(t)
	socketPath = filepath.Join(dir, "agent.sock")

	keyring := sshagent.NewKeyring()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
	})
	go func() {
		_ = agent.ListenAndServe(ctx, socketPath, keyring.(sshagent.ExtendedAgent))
	}()

	var conn net.Conn
	var err error
	for i := 0; i < 50; i++ {
		conn, err = net.Dial("unix", socketPath)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial agent socket: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return socketPath, sshagent.NewClient(conn)
}

// startTestVaultAgent starts an in-process vault-backed SSH agent, unlocked with passphrase.
func startTestVaultAgent(t *testing.T, passphrase []byte) (socketPath string, store *vault.VaultStore) {
	t.Helper()
	dir := unixSocketTempDir(t)
	socketPath = filepath.Join(dir, "agent.sock")

	vaultPath := filepath.Join(dir, "vault.json")
	var err error
	store, err = vault.Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(store, passphrase); err != nil {
		t.Fatal(err)
	}
	va := vault.NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
	})
	go func() {
		_ = agent.ListenAndServe(ctx, socketPath, va)
	}()

	var conn net.Conn
	for i := 0; i < 50; i++ {
		conn, err = net.Dial("unix", socketPath)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial agent socket: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return socketPath, store
}

// writeSyncTestKey generates an ed25519 key with the given comment and writes it to dir.
func writeSyncTestKey(t *testing.T, dir, filename, comment string) (path, fingerprint string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		t.Fatal(err)
	}
	privPath := filepath.Join(dir, filename)
	if err := os.WriteFile(privPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return privPath, ssh.FingerprintSHA256(signer.PublicKey())
}

func TestSyncAgent_NilSession(t *testing.T) {
	result := SyncAgent(nil, "SHA256:whatever", "/does/not/matter", "comment")
	if result.Reloaded || result.VaultSynced || result.ReloadErr != nil || result.VaultErr != nil {
		t.Errorf("expected zero result for nil session, got %+v", result)
	}
}

func TestSyncAgent_ReloadsLoadedKey(t *testing.T) {
	dir := t.TempDir()
	privPath, fp := writeSyncTestKey(t, dir, "id_ed25519", "before")

	socketPath, client := startTestKeyringAgent(t)
	keyData, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatal(err)
	}
	rawKey, err := ssh.ParseRawPrivateKey(keyData)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Add(sshagent.AddedKey{PrivateKey: rawKey, Comment: "before"}); err != nil {
		t.Fatal(err)
	}

	// Rewrite the file with the new comment, as callers must before calling SyncAgent.
	if err := os.WriteFile(privPath, mustMarshalPEM(t, rawKey, "after"), 0o600); err != nil {
		t.Fatal(err)
	}

	session, err := agent.Open(socketPath)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	defer session.Close()

	result := SyncAgent(session, fp, privPath, "after")
	if result.ReloadErr != nil {
		t.Fatalf("unexpected reload error: %v", result.ReloadErr)
	}
	if !result.Reloaded {
		t.Error("expected Reloaded to be true for a key already loaded in the agent")
	}
	if result.VaultSynced {
		t.Error("expected VaultSynced to be false for a non-vault backend")
	}

	keysAfter, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keysAfter) != 1 {
		t.Fatalf("expected 1 key in agent after reload, got %d", len(keysAfter))
	}
	if keysAfter[0].Comment != "after" {
		t.Errorf("agent comment after reload: got %q, want %q", keysAfter[0].Comment, "after")
	}
}

func TestSyncAgent_SkipsUnloadedKey(t *testing.T) {
	dir := t.TempDir()
	privPath, fp := writeSyncTestKey(t, dir, "id_ed25519", "before")

	socketPath, client := startTestKeyringAgent(t)
	// Note: the key at privPath is never added to the agent.

	session, err := agent.Open(socketPath)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	defer session.Close()

	result := SyncAgent(session, fp, privPath, "after")
	if result.ReloadErr != nil {
		t.Fatalf("unexpected reload error: %v", result.ReloadErr)
	}
	if result.Reloaded {
		t.Error("expected Reloaded to be false for a key not loaded in the agent")
	}

	keysAfter, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keysAfter) != 0 {
		t.Fatalf("expected agent to remain empty, got %d keys", len(keysAfter))
	}
}

func TestSyncAgent_VaultBackend_SetsCommentAfterReload(t *testing.T) {
	dir := t.TempDir()
	privPath, fp := writeSyncTestKey(t, dir, "id_ed25519", "before-vault")

	socketPath, store := startTestVaultAgent(t, []byte("editcomment-sync-test"))
	session, err := agent.Open(socketPath)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	defer session.Close()
	if err := vault.AddPrivateKeyFile(session, privPath, true); err != nil {
		t.Fatalf("add private key file: %v", err)
	}

	result := SyncAgent(session, fp, privPath, "after-vault")
	if result.ReloadErr != nil {
		t.Fatalf("unexpected reload error: %v", result.ReloadErr)
	}
	if result.VaultErr != nil {
		t.Fatalf("unexpected vault error: %v", result.VaultErr)
	}
	if !result.Reloaded {
		t.Error("expected Reloaded to be true")
	}
	if !result.VaultSynced {
		t.Error("expected VaultSynced to be true for a vault backend")
	}

	ids := store.AllIdentities()
	if len(ids) != 1 {
		t.Fatalf("expected 1 identity in vault, got %d", len(ids))
	}
	if ids[0].Comment != "after-vault" {
		t.Errorf("vault comment: got %q, want %q", ids[0].Comment, "after-vault")
	}
}

func mustMarshalPEM(t *testing.T, rawKey interface{}, comment string) []byte {
	t.Helper()
	block, err := ssh.MarshalPrivateKey(rawKey, comment)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(block)
}
