package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func TestAgentAuth_Authorized(t *testing.T) {
	_, priv1, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer1, err := ssh.NewSignerFromKey(priv1)
	if err != nil {
		t.Fatal(err)
	}
	pub1 := signer1.PublicKey()

	_, priv2, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer2, err := ssh.NewSignerFromKey(priv2)
	if err != nil {
		t.Fatal(err)
	}
	pub2 := signer2.PublicKey()

	keyring := sshagent.NewKeyring()
	if err := keyring.Add(sshagent.AddedKey{PrivateKey: priv1}); err != nil {
		t.Fatal(err)
	}

	auth := &AgentAuth{Agent: keyring}

	if !auth.Authorized(pub1) {
		t.Error("expected Authorized(pub1) = true (key in agent)")
	}
	if auth.Authorized(pub2) {
		t.Error("expected Authorized(pub2) = false (key not in agent)")
	}
}

// tempSocketDir returns a short temp directory: a Unix socket path under
// t.TempDir() can exceed the sockaddr_un limit on macOS.
func tempSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sshush-auth-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// serveAgentAt serves keyring on a Unix socket at path until stopped by the
// returned function or by the end of the test.
func serveAgentAt(t *testing.T, path string, keyring sshagent.Agent) (stop func()) {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = sshagent.ServeAgent(keyring, conn)
			}()
		}
	}()
	var once sync.Once
	stop = func() { once.Do(func() { _ = ln.Close(); _ = os.Remove(path) }) }
	t.Cleanup(stop)
	return stop
}

func newAgentWithKey(t *testing.T) (sshagent.Agent, ssh.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := sshagent.NewKeyring()
	if err := keyring.Add(sshagent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return keyring, sshPub
}

func TestSocketAuth_AuthorizesAKeyTheAgentLists(t *testing.T) {
	socketPath := filepath.Join(tempSocketDir(t), "agent.sock")
	keyring, pub := newAgentWithKey(t)
	serveAgentAt(t, socketPath, keyring)

	auth := &SocketAuth{SocketPath: socketPath}
	if !auth.Authorized(pub) {
		t.Error("a key the agent lists should be authorized")
	}
}

func TestSocketAuth_RefusesAKeyTheAgentDoesNotHave(t *testing.T) {
	socketPath := filepath.Join(tempSocketDir(t), "agent.sock")
	keyring, _ := newAgentWithKey(t)
	serveAgentAt(t, socketPath, keyring)
	_, stranger := newAgentWithKey(t)

	auth := &SocketAuth{SocketPath: socketPath}
	if auth.Authorized(stranger) {
		t.Error("a key the agent does not list should be refused")
	}
}

func TestSocketAuth_RefusesEveryoneWhenNoAgentIsListening(t *testing.T) {
	socketPath := filepath.Join(tempSocketDir(t), "agent.sock")
	_, pub := newAgentWithKey(t)

	auth := &SocketAuth{SocketPath: socketPath}
	if auth.Authorized(pub) {
		t.Error("an unreachable agent should authorize nobody")
	}
	if (&SocketAuth{}).Authorized(pub) {
		t.Error("an empty socket path should authorize nobody")
	}
}

// TestSocketAuth_PicksUpAnAgentThatStartsLater is the reason this dials per check
// rather than holding one connection: the server must not need a restart when the
// agent does.
func TestSocketAuth_PicksUpAnAgentThatStartsLater(t *testing.T) {
	socketPath := filepath.Join(tempSocketDir(t), "agent.sock")
	keyring, pub := newAgentWithKey(t)
	auth := &SocketAuth{SocketPath: socketPath}

	if auth.Authorized(pub) {
		t.Fatal("nothing should be authorized before the agent exists")
	}

	stop := serveAgentAt(t, socketPath, keyring)
	if !auth.Authorized(pub) {
		t.Error("the key should be authorized once the agent is up")
	}

	stop()
	if auth.Authorized(pub) {
		t.Error("nothing should be authorized once the agent is gone")
	}

	serveAgentAt(t, socketPath, keyring)
	if !auth.Authorized(pub) {
		t.Error("the key should be authorized again after the agent is replaced")
	}
}
