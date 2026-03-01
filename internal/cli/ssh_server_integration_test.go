package cli

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	glssh "github.com/gliderlabs/ssh"
	"github.com/ollykeran/sshush/internal/agent"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// startTestSSHServer starts a gliderlabs SSH server on a random TCP port.
// The server accepts only the given authorized public key for authentication.
// Returns host:port address.
func startTestSSHServer(t *testing.T, authorizedKey ssh.PublicKey) string {
	t.Helper()

	// Generate a host key for the server
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}

	authorizedBlob := authorizedKey.Marshal()

	server := &glssh.Server{
		Handler: func(s glssh.Session) {
			fmt.Fprintln(s, "authenticated")
		},
		PublicKeyHandler: func(ctx glssh.Context, key glssh.PublicKey) bool {
			clientBlob := key.Marshal()
			return len(clientBlob) == len(authorizedBlob) &&
				subtle.ConstantTimeCompare(clientBlob, authorizedBlob) == 1
		},
	}
	server.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })

	go server.Serve(ln)

	// Wait for server readiness
	addr := ln.Addr().String()
	for i := 0; i < 50; i++ {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	return addr
}

func TestSSHServerIntegration_AgentAuthSuccess(t *testing.T) {
	// 1. Start agent
	socketPath, agentClient := startTestAgent(t)

	// 2. Create and add a key
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	if err := runCreate("ed25519", 0, "ssh-auth-test", keyPath, false); err != nil {
		t.Fatalf("create key: %v", err)
	}
	pubKey, _, _, err := agent.ParseKeyFromPath(keyPath)
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}
	if err := agent.AddKeyFromPath(agentClient, keyPath); err != nil {
		t.Fatalf("add key to agent: %v", err)
	}

	// 3. Start SSH server that accepts this key
	addr := startTestSSHServer(t, pubKey)

	// 4. Connect via agent-backed auth
	agentConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial agent: %v", err)
	}
	defer agentConn.Close()
	agentSigners := sshagent.NewClient(agentConn)

	clientConfig := &ssh.ClientConfig{
		User: "test",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeysCallback(agentSigners.Signers),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	client, err := ssh.Dial("tcp", addr, clientConfig)
	if err != nil {
		t.Fatalf("SSH dial failed (should succeed): %v", err)
	}
	defer client.Close()

	// 5. Run a session to verify full auth
	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	var out bytes.Buffer
	session.Stdout = &out
	if err := session.Run(""); err != nil {
		t.Fatalf("session run: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("authenticated")) {
		t.Errorf("expected 'authenticated' in output, got: %s", out.String())
	}
}

func TestSSHServerIntegration_AgentAuthFailsAfterRemove(t *testing.T) {
	// 1. Start agent
	socketPath, agentClient := startTestAgent(t)

	// 2. Create and add a key
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	if err := runCreate("ed25519", 0, "remove-test", keyPath, false); err != nil {
		t.Fatalf("create key: %v", err)
	}
	pubKey, _, _, err := agent.ParseKeyFromPath(keyPath)
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}
	if err := agent.AddKeyFromPath(agentClient, keyPath); err != nil {
		t.Fatalf("add key: %v", err)
	}

	// 3. Start SSH server that accepts this key
	addr := startTestSSHServer(t, pubKey)

	// 4. Remove the key from the agent
	keys, err := agentClient.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, k := range keys {
		if err := agentClient.Remove(k); err != nil {
			t.Fatalf("remove: %v", err)
		}
	}

	// 5. Attempt to connect - should fail
	agentConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial agent: %v", err)
	}
	defer agentConn.Close()
	agentSigners := sshagent.NewClient(agentConn)

	clientConfig := &ssh.ClientConfig{
		User: "test",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeysCallback(agentSigners.Signers),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	_, err = ssh.Dial("tcp", addr, clientConfig)
	if err == nil {
		t.Fatal("SSH dial should fail after key removal, but succeeded")
	}
}

func TestSSHServerIntegration_WrongKeyRejected(t *testing.T) {
	// 1. Start agent with one key
	socketPath, agentClient := startTestAgent(t)

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	if err := runCreate("ed25519", 0, "wrong-key", keyPath, false); err != nil {
		t.Fatalf("create key: %v", err)
	}
	if err := agent.AddKeyFromPath(agentClient, keyPath); err != nil {
		t.Fatalf("add key: %v", err)
	}

	// 2. Start SSH server that accepts a DIFFERENT key
	_, differentPriv, _ := ed25519.GenerateKey(rand.Reader)
	differentSigner, _ := ssh.NewSignerFromKey(differentPriv)
	differentPub := differentSigner.PublicKey()
	addr := startTestSSHServer(t, differentPub)

	// 3. Attempt to connect - should fail
	agentConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial agent: %v", err)
	}
	defer agentConn.Close()
	agentSigners := sshagent.NewClient(agentConn)

	clientConfig := &ssh.ClientConfig{
		User: "test",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeysCallback(agentSigners.Signers),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	_, err = ssh.Dial("tcp", addr, clientConfig)
	if err == nil {
		t.Fatal("SSH dial should fail with wrong key, but succeeded")
	}
}
