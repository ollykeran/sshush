package vault

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ollykeran/sshush/internal/agent"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func unixSocketTempDirListen(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "sshush-vault-ls-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// startVaultListenServer starts ListenAndServe with a vault backend holding one ed25519 key.
func startVaultListenServer(t *testing.T, keyComment string) (socketPath string, client sshagent.Agent) {
	t.Helper()
	dir := unixSocketTempDirListen(t)
	socketPath = filepath.Join(dir, "agent.sock")
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("test-passphrase")
	if err := Init(store, passphrase); err != nil {
		t.Fatal(err)
	}
	va := NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := va.Add(sshagent.AddedKey{PrivateKey: priv, Comment: keyComment}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
		os.Remove(socketPath)
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
		t.Fatalf("dial socket: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return socketPath, sshagent.NewClient(conn)
}

func TestListenAndServe_ListKeys_vault(t *testing.T) {
	_, client := startVaultListenServer(t, "test")
	keys, err := client.List()
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("want 1 key, got %d", len(keys))
	}
	if keys[0].Comment != "test" {
		t.Errorf("comment: want %q, got %q", "test", keys[0].Comment)
	}
}

func TestListenAndServe_Sign_vault(t *testing.T) {
	_, client := startVaultListenServer(t, "sign-test")
	keys, err := client.List()
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("want 1 key, got %d", len(keys))
	}
	data := []byte("data to sign")
	sig, err := client.Sign(keys[0], data)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := keys[0].Verify(data, sig); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}
