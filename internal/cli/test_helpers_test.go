package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ollykeran/sshush/internal/agent"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// writeTestKey generates an ed25519 key with the given comment and writes
// private + public files to dir. Returns the private key path.
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

	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pubAuth := ssh.MarshalAuthorizedKey(signer.PublicKey())
	pubPath := privPath + ".pub"
	if err := os.WriteFile(pubPath, pubAuth, 0o644); err != nil {
		t.Fatal(err)
	}
	return privPath
}

// startTestAgent starts an in-process SSH agent on a temp unix socket.
// Returns the socket path and an agent client. The agent is shut down
// when the test completes.
func startTestAgent(t *testing.T) (socketPath string, client sshagent.ExtendedAgent) {
	t.Helper()
	dir := t.TempDir()
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

// writeFakeEditor creates a shell script at dir/name that writes newComment
// into its first argument file. Returns the script path.
func writeFakeEditor(t *testing.T, dir, name, newComment string) string {
	t.Helper()
	script := "#!/bin/sh\nprintf '%s' '" + newComment + "' > \"$1\"\n"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeFailingEditor creates a shell script that exits with code 1.
func writeFailingEditor(t *testing.T, dir, name string) string {
	t.Helper()
	script := "#!/bin/sh\nexit 1\n"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
