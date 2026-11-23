package tray

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ssh "golang.org/x/crypto/ssh"
)

func TestSuggestKeyPathSkipsTakenNames(t *testing.T) {
	dir := t.TempDir()
	if got := suggestKeyPath(dir, "ed25519"); got != filepath.Join(dir, "id_ed25519") {
		t.Errorf("empty dir: got %s", got)
	}
	os.WriteFile(filepath.Join(dir, "id_ed25519"), nil, 0o600)
	os.WriteFile(filepath.Join(dir, "id_ed25519_2.pub"), nil, 0o600) // a stray .pub counts as taken
	if got := suggestKeyPath(dir, "ed25519"); got != filepath.Join(dir, "id_ed25519_3") {
		t.Errorf("taken names: got %s", got)
	}
}

func TestCreateKeyWritesAPairAndRefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "id_test")
	if err := CreateKey(0, "me@host", path); err != nil {
		t.Fatal(err)
	}
	priv, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.ParsePrivateKey(priv)
	if err != nil {
		t.Fatalf("private key does not parse: %v", err)
	}
	pub, _ := os.ReadFile(path + ".pub")
	if !strings.HasPrefix(string(pub), "ssh-ed25519 ") || !strings.Contains(string(pub), "me@host") {
		t.Errorf(".pub: got %q", pub)
	}
	if signer.PublicKey().Type() != ssh.KeyAlgoED25519 {
		t.Errorf("type: got %s", signer.PublicKey().Type())
	}
	if err := CreateKey(0, "again", path); !errors.Is(err, errKeyExists) {
		t.Errorf("overwrite: got %v", err)
	}
}

func TestCreateKeyRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		choice        int
		comment, path string
	}{
		{len(keyChoices), "c", filepath.Join(dir, "a")},
		{0, "two\nlines", filepath.Join(dir, "b")},
		{0, "c", ""},
	} {
		if err := CreateKey(tc.choice, tc.comment, tc.path); err == nil {
			t.Errorf("%+v: no error", tc)
		}
	}
}
