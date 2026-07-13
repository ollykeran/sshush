package agent

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

	sshagent "golang.org/x/crypto/ssh/agent"
)

func unixSocketTempDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "sshush-a-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// startServerKeyring starts ListenAndServe with an in-memory keyring,
// adds one key with the given comment, and returns a connected client.
func startServerKeyring(t *testing.T, keyComment string) (socketPath string, client sshagent.Agent) {
	t.Helper()
	dir := unixSocketTempDir(t)
	socketPath = filepath.Join(dir, "agent.sock")
	keyring := sshagent.NewKeyring()
	ext := keyring.(sshagent.ExtendedAgent)
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := ext.Add(sshagent.AddedKey{PrivateKey: priv, Comment: keyComment}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
		os.Remove(socketPath)
	})
	go func() {
		_ = ListenAndServe(ctx, socketPath, ext)
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

func TestListenAndServe_ListKeys(t *testing.T) {
	_, client := startServerKeyring(t, "test")
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

func TestListenAndServe_Sign(t *testing.T) {
	_, client := startServerKeyring(t, "sign-test")
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
