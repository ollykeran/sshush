package cli

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	sshagent "golang.org/x/crypto/ssh/agent"
)

func TestListKeysTo(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := sshagent.NewKeyring()
	err = keyring.Add(sshagent.AddedKey{PrivateKey: priv, Comment: "test-comment"})
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err = ListKeysTo(keyring, &buf)
	if err != nil {
		t.Fatalf("ListKeysTo: %v", err)
	}
	out := buf.String()
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	if !bytes.Contains(buf.Bytes(), []byte("test-comment")) {
		t.Errorf("output should contain comment %q: %s", "test-comment", out)
	}
	if !bytes.Contains(buf.Bytes(), []byte("SHA256:")) {
		t.Errorf("output should contain SHA256 fingerprint: %s", out)
	}
	if !bytes.Contains(buf.Bytes(), []byte("ssh-ed25519")) {
		t.Errorf("output should contain key type: %s", out)
	}
}

func TestListKeysTo_Empty(t *testing.T) {
	keyring := sshagent.NewKeyring()
	var buf bytes.Buffer
	err := ListKeysTo(keyring, &buf)
	if err != nil {
		t.Fatalf("ListKeysTo: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("no keys loaded")) {
		t.Errorf("expected 'no keys loaded' message, got: %s", buf.String())
	}
}
