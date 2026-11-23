//go:build !windows

package tray

import (
	"context"
	"crypto/ed25519"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/vault"
	"github.com/ollykeran/sshush/internal/vaultops"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// serveVault serves an initialized, unlocked vault and returns its socket.
func serveVault(t *testing.T) string {
	t.Helper()
	// Not t.TempDir(): its path can be too long for a Unix socket on macOS.
	dir, err := os.MkdirTemp("/tmp", "sshush-tray-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	store, err := vault.Open(filepath.Join(dir, "vault.json"))
	if err != nil {
		t.Fatal(err)
	}
	pass := []byte("tray-test")
	if err := vault.Init(store, pass); err != nil {
		t.Fatal(err)
	}
	va := vault.NewVaultAgent(store)
	if err := va.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "agent.sock")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ready := make(chan struct{})
	go func() { _ = agent.ListenAndServe(ctx, sock, va, agent.WithReady(func() { close(ready) })) }()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("vault agent did not become ready")
	}
	return sock
}

// asker answers each passphrase prompt with the next of answers; an empty
// answer cancels. It counts the prompts and records each wrong flag.
type asker struct {
	answers []string
	wrong   []bool
}

func (a *asker) ask(_ string, wrong bool) ([]byte, error) {
	a.wrong = append(a.wrong, wrong)
	if len(a.answers) == 0 || a.answers[0] == "" {
		return nil, errCancelled
	}
	next := a.answers[0]
	a.answers = a.answers[1:]
	return []byte(next), nil
}

func listComments(t *testing.T, sock string) []string {
	t.Helper()
	s, err := agent.Open(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	keys, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var c []string
	for _, k := range keys {
		c = append(c, k.Comment)
	}
	return c
}

// writeKey writes an OpenSSH private key file, encrypted when passphrase is
// not empty, and returns its path and fingerprint.
func writeKey(t *testing.T, comment, passphrase string) (path, fingerprint string) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	var block *pem.Block
	var err error
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, comment)
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, comment, []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(t.TempDir(), "id_"+comment)
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	sshPub, _ := ssh.NewPublicKey(pub)
	return path, ssh.FingerprintSHA256(sshPub)
}

// writePub writes the public half of the key file at path beside it, with comment.
func writePub(t *testing.T, path, comment string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.ParseRawPrivateKeyWithPassphrase(data, []byte("right"))
	if err != nil {
		key, err = ssh.ParseRawPrivateKey(data)
	}
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " " + comment + "\n"
	if err := os.WriteFile(path+".pub", []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAddKeysAddsAndReportsEachFailure(t *testing.T) {
	kr := sshagent.NewKeyring()
	sock := serveKeyring(t, kr)
	good, _ := writeKey(t, "good", "")
	locked, _ := writeKey(t, "locked", "secret")
	missing := filepath.Join(t.TempDir(), "id_missing")

	added, err := AddKeys(sock, []string{good, locked, missing}, (&asker{}).ask)
	if added != 1 {
		t.Errorf("added: got %d, want 1", added)
	}
	if err == nil || !strings.Contains(err.Error(), "id_locked: skipped") ||
		!strings.Contains(err.Error(), "id_missing") {
		t.Errorf("error: got %v", err)
	}
	if keys, _ := kr.List(); len(keys) != 1 || keys[0].Comment != "good" {
		t.Errorf("agent holds %+v", keys)
	}
}

func TestUnloadKeyFromKeyring(t *testing.T) {
	kr := sshagent.NewKeyring()
	sock := serveKeyring(t, kr)
	path, fp := writeKey(t, "gone", "")
	if _, err := AddKeys(sock, []string{path}, nil); err != nil {
		t.Fatal(err)
	}
	if err := UnloadKey(sock, fp); err != nil {
		t.Fatalf("unload: %v", err)
	}
	if keys, _ := kr.List(); len(keys) != 0 {
		t.Errorf("agent still holds %+v", keys)
	}
	if err := UnloadKey(sock, fp); err == nil {
		t.Error("unloading a key the agent no longer holds: want an error")
	}
}

func TestEncryptedKeyRetriesAWrongPassphrase(t *testing.T) {
	kr := sshagent.NewKeyring()
	sock := serveKeyring(t, kr)
	path, _ := writeKey(t, "enc", "right")
	writePub(t, path, "from-pub")
	a := &asker{answers: []string{"wrong", "right"}}
	if added, err := AddKeys(sock, []string{path}, a.ask); added != 1 || err != nil {
		t.Fatalf("added %d, err %v", added, err)
	}
	if len(a.wrong) != 2 || a.wrong[0] || !a.wrong[1] {
		t.Errorf("prompts: got wrong flags %v, want [false true]", a.wrong)
	}
	if got := listComments(t, sock); len(got) != 1 || got[0] != "from-pub" {
		t.Errorf("comment: got %v, want the one from the .pub file", got)
	}
}

func TestEncryptedKeyIgnoresAMismatchedPub(t *testing.T) {
	kr := sshagent.NewKeyring()
	sock := serveKeyring(t, kr)
	path, _ := writeKey(t, "enc", "right")
	other, _ := writeKey(t, "other", "")
	writePub(t, other, "not-this-key")
	data, _ := os.ReadFile(other + ".pub")
	if err := os.WriteFile(path+".pub", data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AddKeys(sock, []string{path}, (&asker{answers: []string{"right"}}).ask); err != nil {
		t.Fatal(err)
	}
	if got := listComments(t, sock); len(got) != 1 || got[0] != "id_enc" {
		t.Errorf("comment: got %v, want the file name", got)
	}
}

func TestEncryptedKeyIntoVault(t *testing.T) {
	sock := serveVault(t)
	path, fp := writeKey(t, "vaulted", "right")
	if added, err := AddKeys(sock, []string{path}, (&asker{answers: []string{"right"}}).ask); added != 1 || err != nil {
		t.Fatalf("added %d, err %v", added, err)
	}
	if got := listComments(t, sock); len(got) != 1 || got[0] != "id_vaulted" {
		t.Errorf("vault lists %v, want the file name as comment", got)
	}
	rows := ProbeMain(vaultops.Env{SocketPath: sock}).Rows()
	if len(rows) != 1 || rows[0].Fingerprint != fp {
		t.Errorf("vault identities: got %+v", rows)
	}
}

func TestUnloadFromVaultKeepsTheIdentity(t *testing.T) {
	sock := serveVault(t)
	path, fp := writeKey(t, "kept", "")
	if _, err := AddKeys(sock, []string{path}, nil); err != nil {
		t.Fatal(err)
	}
	if err := UnloadKey(sock, fp); err != nil {
		t.Fatalf("unload: %v", err)
	}
	if v := ViewOf(Probe(sock), true); len(v.Keys) != 0 {
		t.Fatalf("after unload the agent still serves %+v", v.Keys)
	}
	rows := ProbeMain(vaultops.Env{SocketPath: sock}).Rows()
	if len(rows) != 1 || rows[0].Fingerprint != fp || rows[0].Loaded != vaultops.LoadNo {
		t.Fatalf("after unload the vault lists %+v", rows)
	}
}

func TestAddKeysAgentDown(t *testing.T) {
	if _, err := AddKeys(filepath.Join(t.TempDir(), "nothing.sock"), []string{"x"}, nil); err == nil {
		t.Error("want an error when no agent answers")
	}
}
