package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh/agent"
)

func TestListenAndServe_ListKeys(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "agent.sock")

	keyring := agent.NewKeyring()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	err = keyring.Add(agent.AddedKey{PrivateKey: priv, Comment: "test"})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = ListenAndServe(ctx, socketPath, keyring)
	}()

	// Wait for server to listen
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
	defer conn.Close()

	client := agent.NewClient(conn)
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

	cancel()
	time.Sleep(50 * time.Millisecond)
	os.Remove(socketPath)
}
