package vault

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"

	sshagent "golang.org/x/crypto/ssh/agent"
)

func TestResolveIdentity_ambiguousComment(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "v.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Init(store, []byte("test-pass")); err != nil {
		t.Fatal(err)
	}
	va := NewVaultAgent(store)
	if err := va.Unlock([]byte("test-pass")); err != nil {
		t.Fatal(err)
	}
	_, p1, _ := ed25519.GenerateKey(rand.Reader)
	_, p2, _ := ed25519.GenerateKey(rand.Reader)
	if err := va.Add(sshagent.AddedKey{PrivateKey: p1, Comment: "dup"}); err != nil {
		t.Fatal(err)
	}
	if err := va.Add(sshagent.AddedKey{PrivateKey: p2, Comment: "dup"}); err != nil {
		t.Fatal(err)
	}
	_, err = ResolveIdentity(store, "dup")
	if !errors.Is(err, ErrAmbiguousComment) {
		t.Fatalf("want ErrAmbiguousComment, got %v", err)
	}
}
