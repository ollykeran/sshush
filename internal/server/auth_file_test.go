package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestFileAuth_Authorized(t *testing.T) {
	_, priv1, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer1, err := ssh.NewSignerFromKey(priv1)
	if err != nil {
		t.Fatal(err)
	}
	pub1 := signer1.PublicKey()
	line1 := string(ssh.MarshalAuthorizedKey(pub1))

	_, priv2, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer2, err := ssh.NewSignerFromKey(priv2)
	if err != nil {
		t.Fatal(err)
	}
	pub2 := signer2.PublicKey()

	dir := t.TempDir()
	path := filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(path, []byte(line1), 0600); err != nil {
		t.Fatal(err)
	}

	auth, err := NewFileAuth(path)
	if err != nil {
		t.Fatal(err)
	}

	if !auth.Authorized(pub1) {
		t.Error("expected Authorized(pub1) = true (key in file)")
	}
	if auth.Authorized(pub2) {
		t.Error("expected Authorized(pub2) = false (key not in file)")
	}
}

func TestFileAuth_InvalidFile(t *testing.T) {
	_, err := NewFileAuth("/nonexistent/path")
	if err == nil {
		t.Error("expected error when file does not exist")
	}
}
