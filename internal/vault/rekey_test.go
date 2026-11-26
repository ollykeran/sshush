package vault

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ollykeran/sshush/internal/agent"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// rekeyFixture is an unlocked vault agent with recovery and two keys.
type rekeyFixture struct {
	path     string
	store    *VaultStore
	va       *VaultAgent
	pass     []byte
	mnemonic string
	pubs     []ssh.PublicKey
}

func newRekeyFixture(t *testing.T) *rekeyFixture {
	t.Helper()
	f := &rekeyFixture{path: filepath.Join(t.TempDir(), "vault.json"), pass: []byte("old-passphrase")}
	var err error
	if f.store, err = Open(f.path); err != nil {
		t.Fatal(err)
	}
	if err := Init(f.store, f.pass); err != nil {
		t.Fatal(err)
	}
	if f.mnemonic, err = GenerateRecoveryMnemonic(); err != nil {
		t.Fatal(err)
	}
	if err := EnableRecoveryWithPassphrase(f.store, f.pass, f.mnemonic); err != nil {
		t.Fatal(err)
	}
	f.va = NewVaultAgent(f.store)
	if err := f.va.Unlock(f.pass); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"a@test", "b@test"} {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.va.addKeyWithAutoload(sshagent.AddedKey{PrivateKey: priv, Comment: c}, true, ""); err != nil {
			t.Fatal(err)
		}
		sshPub, _ := ssh.NewPublicKey(pub)
		f.pubs = append(f.pubs, sshPub)
	}
	return f
}

// signsWithEvery checks va signs with every key in the fixture.
func (f *rekeyFixture) signsWithEvery(t *testing.T, va *VaultAgent, what string) {
	t.Helper()
	for _, pub := range f.pubs {
		sig, err := va.Sign(pub, []byte("data"))
		if err != nil {
			t.Fatalf("%s: sign: %v", what, err)
		}
		if err := pub.Verify([]byte("data"), sig); err != nil {
			t.Fatalf("%s: verify: %v", what, err)
		}
	}
}

// reopened is a fresh agent on the vault file, as after a restart.
func (f *rekeyFixture) reopened(t *testing.T) *VaultAgent {
	t.Helper()
	store, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	return NewVaultAgent(store)
}

func TestChangePassphrase_overTheOp(t *testing.T) {
	f := newRekeyFixture(t)
	newPass := []byte("new-passphrase")
	newPhrase, _ := GenerateRecoveryMnemonic()

	_, err := callOp(t, f.va, agent.OpChangePassphrase, BuildChangePassphrasePayload(Credential{Passphrase: f.pass}, newPass, newPhrase))
	if err != nil {
		t.Fatal(err)
	}
	// The running agent carries on unlocked, under the new key.
	f.signsWithEvery(t, f.va, "running agent")

	// After a restart only the new secrets open it.
	if err := f.reopened(t).Unlock(f.pass); err == nil {
		t.Error("old passphrase still unlocks")
	}
	if err := f.reopened(t).UnlockWithRecovery(f.mnemonic); err == nil {
		t.Error("old recovery phrase still unlocks")
	}
	byPhrase := f.reopened(t)
	if err := byPhrase.UnlockWithRecovery(newPhrase); err != nil {
		t.Fatalf("new recovery phrase: %v", err)
	}
	f.signsWithEvery(t, byPhrase, "unlocked by the new phrase")
	byPass := f.reopened(t)
	if err := byPass.Unlock(newPass); err != nil {
		t.Fatalf("new passphrase: %v", err)
	}
	f.signsWithEvery(t, byPass, "unlocked by the new passphrase")
	if got := byPass.store.GetMetadata().CreatedAt; got == "" || got != f.store.GetMetadata().CreatedAt {
		t.Errorf("created date %q not kept", got)
	}
}

func TestChangePassphrase_withTheRecoveryPhraseAndNoNewOne(t *testing.T) {
	f := newRekeyFixture(t)
	newPass := []byte("new-passphrase")
	if err := f.va.changePassphrase(Credential{Mnemonic: "  " + f.mnemonic + "\n"}, newPass, ""); err != nil {
		t.Fatal(err)
	}
	if Describe(f.store).Recovery {
		t.Error("recovery still set up")
	}
	if err := f.reopened(t).UnlockWithRecovery(f.mnemonic); !errors.Is(err, errNoRecovery) {
		t.Errorf("old phrase: got %v, want no recovery", err)
	}
	byPass := f.reopened(t)
	if err := byPass.Unlock(newPass); err != nil {
		t.Fatal(err)
	}
	f.signsWithEvery(t, byPass, "new passphrase")
}

func TestChangePassphrase_refusalsLeaveTheVaultAlone(t *testing.T) {
	f := newRekeyFixture(t)
	before, _ := os.ReadFile(f.path)
	good, _ := GenerateRecoveryMnemonic()
	cases := []struct {
		name    string
		cred    Credential
		newPass string
		phrase  string
		status  error
	}{
		{"wrong passphrase", Credential{Passphrase: []byte("not-the-passphrase")}, "new-passphrase", good, agent.ErrWrongPassphrase},
		{"wrong phrase", Credential{Mnemonic: good}, "new-passphrase", good, agent.ErrWrongPassphrase},
		{"weak new passphrase", Credential{Passphrase: f.pass}, "short", good, agent.ErrBadRequest},
		{"bad new phrase", Credential{Passphrase: f.pass}, "new-passphrase", "not a phrase", agent.ErrBadRequest},
	}
	for _, tc := range cases {
		_, err := callOp(t, f.va, agent.OpChangePassphrase, BuildChangePassphrasePayload(tc.cred, []byte(tc.newPass), tc.phrase))
		if !errors.Is(err, tc.status) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.status)
		}
		if after, _ := os.ReadFile(f.path); !bytes.Equal(before, after) {
			t.Fatalf("%s: vault file changed", tc.name)
		}
	}
	f.signsWithEvery(t, f.va, "after the refusals")
}

func TestChangePassphrase_failurePartWayLeavesTheVaultWhole(t *testing.T) {
	f := newRekeyFixture(t)
	// Damage the second identity's blob, in memory only: the first is
	// re-encrypted before the second fails to decrypt.
	ids := f.store.AllIdentities()
	ids[1].EncryptedBlob[len(ids[1].EncryptedBlob)-1] ^= 0xFF
	f.store.mu.Lock()
	f.store.identities = ids
	f.store.mu.Unlock()
	before, _ := os.ReadFile(f.path)
	metaBefore := f.store.GetMetadata()

	if _, err := ChangePassphrase(f.store, Credential{Passphrase: f.pass}, []byte("new-passphrase"), ""); err == nil {
		t.Fatal("no error")
	}
	if after, _ := os.ReadFile(f.path); !bytes.Equal(before, after) {
		t.Error("vault file changed")
	}
	if !bytes.Equal(f.store.GetMetadata().Salt, metaBefore.Salt) {
		t.Error("in-memory vault changed")
	}
	if !VerifyPassphrase(f.store, f.pass) {
		t.Error("old passphrase no longer opens it")
	}
}

func TestChangePassphrase_lockedAgentStaysLocked(t *testing.T) {
	f := newRekeyFixture(t)
	if err := f.va.Lock(nil); err != nil {
		t.Fatal(err)
	}
	if err := f.va.changePassphrase(Credential{Passphrase: f.pass}, []byte("new-passphrase"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := callOp(t, f.va, agent.OpListIdentities, nil); !errors.Is(err, agent.ErrVaultLocked) {
		t.Errorf("got %v, want still locked", err)
	}
	if err := f.va.Unlock([]byte("new-passphrase")); err != nil {
		t.Fatal(err)
	}
	f.signsWithEvery(t, f.va, "after unlocking")
}

func TestSetRecovery(t *testing.T) {
	f := newRekeyFixture(t)
	newPhrase, _ := GenerateRecoveryMnemonic()
	if _, err := callOp(t, f.va, agent.OpSetRecovery, BuildSetRecoveryPayload(Credential{Passphrase: []byte("wrong-passphrase")}, newPhrase)); !errors.Is(err, agent.ErrWrongPassphrase) {
		t.Fatalf("wrong passphrase: got %v", err)
	}
	if _, err := callOp(t, f.va, agent.OpSetRecovery, BuildSetRecoveryPayload(Credential{Passphrase: f.pass}, newPhrase)); err != nil {
		t.Fatal(err)
	}
	if err := f.reopened(t).UnlockWithRecovery(f.mnemonic); err == nil {
		t.Error("old phrase still unlocks")
	}
	byPhrase := f.reopened(t)
	if err := byPhrase.UnlockWithRecovery(newPhrase); err != nil {
		t.Fatal(err)
	}
	f.signsWithEvery(t, byPhrase, "new phrase")
	if err := f.reopened(t).Unlock(f.pass); err != nil {
		t.Errorf("passphrase changed: %v", err)
	}

	// A vault made without recovery can be given a phrase.
	if err := SetRecovery(f.store, Credential{Passphrase: f.pass}, ""); !errors.Is(err, ErrBadMnemonic) {
		t.Errorf("empty phrase: got %v", err)
	}
}
