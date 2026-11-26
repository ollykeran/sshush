package cygsock

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	sshagent "golang.org/x/crypto/ssh/agent"
)

// serveKeyring serves a keyring holding one key with comment on a socket file
// in a temporary directory, and returns the file's path once it is there.
func serveKeyring(t *testing.T, comment string) string {
	t.Helper()
	ring := sshagent.NewKeyring()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.Add(sshagent.AddedKey{PrivateKey: priv, Comment: comment}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "agent.sock")
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, path, func(c net.Conn) { _ = sshagent.ServeAgent(ring, c); c.Close() }, func() { close(ready) })
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Serve: %v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("socket file left behind: %v", err)
		}
	})
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("Serve: %v", err)
	}
	return path
}

// dial does what a Cygwin client does: reads the socket file, connects and
// runs its half of the handshake with secret in place of the file's.
func dial(t *testing.T, path string, secret *Secret) (net.Conn, error) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	var w [4]uint32
	if _, err := fmt.Sscanf(string(data), "!<socket >%d s %08x-%08x-%08x-%08x", &port, &w[0], &w[1], &w[2], &w[3]); err != nil {
		t.Fatalf("socket file %q: %v", data, err)
	}
	var s Secret
	for i := range w {
		binary.LittleEndian.PutUint32(s[4*i:], w[i])
	}
	if secret != nil {
		s = *secret
	}
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(s[:]); err != nil {
		return nil, err
	}
	var echo Secret
	if _, err := io.ReadFull(conn, echo[:]); err != nil {
		conn.Close()
		return nil, err
	}
	if echo != s {
		t.Fatalf("server echoed %x, want %x", echo, s)
	}
	cred := []byte{1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0}
	if _, err := conn.Write(cred); err != nil {
		return nil, err
	}
	var reply [12]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		return nil, err
	}
	if got := binary.LittleEndian.Uint32(reply[:]); got != uint32(os.Getpid()) {
		t.Errorf("server pid %d, want %d", got, os.Getpid())
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func TestServe_aClientWithTheSecretReachesTheAgent(t *testing.T) {
	path := serveKeyring(t, "cygwin-key")
	conn, err := dial(t, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	keys, err := sshagent.NewClient(conn).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].Comment != "cygwin-key" {
		t.Fatalf("keys %v", keys)
	}
}

func TestServe_aWrongSecretIsHungUpOn(t *testing.T) {
	path := serveKeyring(t, "x")
	var wrong Secret
	if _, err := dial(t, path, &wrong); err == nil {
		t.Fatal("the handshake went through with the wrong secret")
	}
}

func TestServe_theFileIsPrivateAndFresh(t *testing.T) {
	path := serveKeyring(t, "x")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(string(data), "!<socket >%d s ", &port); err != nil || port == 0 {
		t.Fatalf("socket file %q", data)
	}
}

// An agent's file is left alone by another's exit.
func TestRemoveIfOurs_leavesAnotherAgentsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.sock")
	if err := os.WriteFile(path, []byte("!<socket >1 s other"), 0o600); err != nil {
		t.Fatal(err)
	}
	removeIfOurs(path, "!<socket >2 s mine")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("another agent's file was removed: %v", err)
	}
}
