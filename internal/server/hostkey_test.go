package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func TestEnsureHostKey_CreatesAKeyTheFirstTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host_ed25519")

	created, err := EnsureHostKey(path)
	if err != nil {
		t.Fatalf("EnsureHostKey: %v", err)
	}
	if !created {
		t.Error("EnsureHostKey should report that it created the key")
	}
	if _, err := HostKeyFingerprint(path); err != nil {
		t.Errorf("generated key is not usable: %v", err)
	}
}

func TestEnsureHostKey_WritesTheKeyReadableOnlyByItsOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host_ed25519")
	if _, err := EnsureHostKey(path); err != nil {
		t.Fatalf("EnsureHostKey: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("host key mode = %o, want 600", perm)
	}
}

func TestEnsureHostKey_KeepsAnExistingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host_ed25519")
	if _, err := EnsureHostKey(path); err != nil {
		t.Fatalf("EnsureHostKey: %v", err)
	}
	first, err := HostKeyFingerprint(path)
	if err != nil {
		t.Fatal(err)
	}

	created, err := EnsureHostKey(path)
	if err != nil {
		t.Fatalf("second EnsureHostKey: %v", err)
	}
	if created {
		t.Error("EnsureHostKey should not report creating a key that already existed")
	}
	second, err := HostKeyFingerprint(path)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("host key changed: %s then %s", first, second)
	}
}

func TestEnsureHostKey_CreatesTheDirectoryItNeeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "host_ed25519")
	if _, err := EnsureHostKey(path); err != nil {
		t.Fatalf("EnsureHostKey: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("host key not written: %v", err)
	}
}

func TestEnsureHostKey_NamesAFileThatIsNotAKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host_ed25519")
	if err := os.WriteFile(path, []byte("not a key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := EnsureHostKey(path)
	if err == nil {
		t.Fatal("EnsureHostKey should reject a file that is not a private key")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error = %q, want it to name %s", err, path)
	}
}

// TestServer_PresentsTheSameHostKeyAcrossRestarts is the check behind the feature:
// a client that pinned the host key on one run must not see it change on the next.
func TestServer_PresentsTheSameHostKeyAcrossRestarts(t *testing.T) {
	hostKeyPath := filepath.Join(t.TempDir(), "host_ed25519")

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	keyring := sshagent.NewKeyring()
	if err := keyring.Add(sshagent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatal(err)
	}

	// run starts a server on its own port, connects, and reports the host key the
	// client was offered.
	run := func() string {
		t.Helper()
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		ln.Close()

		ready := make(chan struct{})
		srv := &Server{
			ListenAddr:  addr,
			AuthKeys:    &AgentAuth{Agent: keyring},
			HostKeyPath: hostKeyPath,
			Ready:       func() { close(ready) },
		}
		go func() { _ = srv.ListenAndServe() }()
		select {
		case <-ready:
		case <-time.After(5 * time.Second):
			t.Fatal("server never became ready")
		}

		var offered string
		conn, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
			User: "test",
			Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
			HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
				offered = ssh.FingerprintSHA256(key)
				return nil
			},
			Timeout: 5 * time.Second,
		})
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		conn.Close()
		return offered
	}

	first := run()
	second := run()
	if first != second {
		t.Errorf("host key changed between runs: %s then %s", first, second)
	}
	onDisk, err := HostKeyFingerprint(hostKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if first != onDisk {
		t.Errorf("server offered %s, want the key on disk %s", first, onDisk)
	}
}

func TestHostKeyFingerprint_MatchesThePublicKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "host_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}

	pub, err := ssh.NewPublicKey(priv.Public())
	if err != nil {
		t.Fatal(err)
	}
	got, err := HostKeyFingerprint(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := ssh.FingerprintSHA256(pub); got != want {
		t.Errorf("HostKeyFingerprint = %s, want %s", got, want)
	}
}
