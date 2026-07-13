package vault

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/keys"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func unixSocketTempDirAddFile(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "sshush-vault-add-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestAddPrivateKeyFileToSocket_keyring(t *testing.T) {
	dir := unixSocketTempDirAddFile(t)
	socketPath := filepath.Join(dir, "agent.sock")
	keyPath := filepath.Join(dir, "id_ed25519")
	privPEM, _, err := keys.Generate("ed25519", 0, "kr")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, privPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	keyring := sshagent.NewKeyring()
	ext := keyring.(sshagent.ExtendedAgent)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
		_ = os.Remove(socketPath)
	})
	go func() { _ = agent.ListenAndServe(ctx, socketPath, ext) }()
	var conn net.Conn
	for i := 0; i < 50; i++ {
		conn, err = net.Dial("unix", socketPath)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	if err := AddPrivateKeyFileToSocket(socketPath, keyPath, true); err != nil {
		t.Fatalf("AddPrivateKeyFileToSocket: %v", err)
	}
	client := sshagent.NewClient(conn)
	got, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("keyring: want 1 key, got %d", len(got))
	}
}

func TestAddPrivateKeyFileToSocket_vaultAutoloadPersistsAfterNewAgent(t *testing.T) {
	dir := unixSocketTempDirAddFile(t)
	socketPath := filepath.Join(dir, "agent.sock")
	vaultPath := filepath.Join(dir, "vault.json")
	keyPath := filepath.Join(dir, "id_ed25519")
	privPEM, _, err := keys.Generate("ed25519", 0, "persist-me")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, privPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	pass := []byte("addpath-test")
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
	go func() { _ = agent.ListenAndServe(ctx, socketPath, va) }()
	var conn net.Conn
	for i := 0; i < 50; i++ {
		conn, err = net.Dial("unix", socketPath)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	if err := AddPrivateKeyFileToSocket(socketPath, keyPath, true); err != nil {
		t.Fatalf("AddPrivateKeyFileToSocket autoload=true: %v", err)
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
		t.Fatalf("new agent List: want 1 key persist-me, got %d keys", len(after))
	}
}

func TestAddPrivateKeyFileToSocket_vaultNoAutoloadNotListedAfterNewAgent(t *testing.T) {
	dir := unixSocketTempDirAddFile(t)
	socketPath := filepath.Join(dir, "agent.sock")
	vaultPath := filepath.Join(dir, "vault.json")
	keyPath := filepath.Join(dir, "id_ed25519")
	privPEM, _, err := keys.Generate("ed25519", 0, "session-only")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, privPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	pass := []byte("addpath-test2")
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
	go func() { _ = agent.ListenAndServe(ctx, socketPath, va) }()
	var conn net.Conn
	for i := 0; i < 50; i++ {
		conn, err = net.Dial("unix", socketPath)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	if err := AddPrivateKeyFileToSocket(socketPath, keyPath, false); err != nil {
		t.Fatalf("AddPrivateKeyFileToSocket autoload=false: %v", err)
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
		t.Fatalf("new agent List: want 0 keys (session-only), got %d", len(after))
	}
}
