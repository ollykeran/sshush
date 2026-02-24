package agent

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func mustMarshalKey(t *testing.T, comment string) []byte {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(block)
}

func TestAddKeyFromPath(t *testing.T) {
	keyPEM := mustMarshalKey(t, "test-key-comment")
	dir := t.TempDir()
	path := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(path, keyPEM, 0600); err != nil {
		t.Fatal(err)
	}

	keyring := agent.NewKeyring()
	if err := AddKeyFromPath(keyring, path); err != nil {
		t.Fatalf("AddKeyFromPath: %v", err)
	}
	keys, err := keyring.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("want 1 key, got %d", len(keys))
	}
	if keys[0].Comment != "test-key-comment" {
		t.Errorf("comment: want %q, got %q", "test-key-comment", keys[0].Comment)
	}
}

func TestAddKeyFromPath_missingFile(t *testing.T) {
	keyring := agent.NewKeyring()
	err := AddKeyFromPath(keyring, filepath.Join(t.TempDir(), "nonexistent"))
	if err == nil {
		t.Fatal("want error for missing file")
	}
}

func TestLoadKeys_skipsBadPaths(t *testing.T) {
	keyPEM := mustMarshalKey(t, "only-valid")
	dir := t.TempDir()
	validPath := filepath.Join(dir, "valid")
	if err := os.WriteFile(validPath, keyPEM, 0600); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(dir, "missing")

	var errOut bytes.Buffer
	keyring := agent.NewKeyring()
	err := LoadKeys(keyring, []string{badPath, validPath}, &errOut)
	if err != nil {
		t.Fatalf("LoadKeys: %v", err)
	}
	if errOut.Len() == 0 {
		t.Error("expected error output for bad path")
	}
	keys, _ := keyring.List()
	if len(keys) != 1 {
		t.Fatalf("want 1 key after skipping bad path, got %d", len(keys))
	}
}
