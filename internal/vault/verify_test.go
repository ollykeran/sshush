package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

const verifyTestPassphrase = "e2epassthatalwaysmeetspasswordrequirementsA1!"

// initVerifyTestStore creates an initialized vault with verifyTestPassphrase,
// returning the store and the file behind it.
func initVerifyTestStore(t *testing.T) (*VaultStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vault.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Init(store, []byte(verifyTestPassphrase)); err != nil {
		t.Fatal(err)
	}
	return store, path
}

func TestVerifyPassphrase_AcceptsTheVaultsPassphrase(t *testing.T) {
	store, _ := initVerifyTestStore(t)
	if !VerifyPassphrase(store, []byte(verifyTestPassphrase)) {
		t.Error("the passphrase the vault was created with was refused")
	}
}

func TestVerifyPassphrase_RejectsAnyOtherPassphrase(t *testing.T) {
	store, _ := initVerifyTestStore(t)
	for _, wrong := range []string{"", "wrong", verifyTestPassphrase + "x", verifyTestPassphrase[1:]} {
		if VerifyPassphrase(store, []byte(wrong)) {
			t.Errorf("VerifyPassphrase(%q) = true, want false", wrong)
		}
	}
}

func TestVerifyPassphrase_RejectsAnUninitializedVault(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "vault.json"))
	if err != nil {
		t.Fatal(err)
	}
	if VerifyPassphrase(store, []byte(verifyTestPassphrase)) {
		t.Error("an uninitialized vault accepted a passphrase")
	}
}

func TestVerifyPassphrase_LeavesTheVaultFileUntouched(t *testing.T) {
	store, path := initVerifyTestStore(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	VerifyPassphrase(store, []byte(verifyTestPassphrase))
	VerifyPassphrase(store, []byte("wrong"))
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("verifying a passphrase changed the vault file")
	}
}

// TestVerifyPassphrase_DoesNotUnlockAnAgent pins the check-only contract: an
// agent over the same vault stays locked, and still unlocks normally afterwards.
func TestVerifyPassphrase_DoesNotUnlockAnAgent(t *testing.T) {
	store, _ := initVerifyTestStore(t)
	agent := NewVaultAgent(store)

	if !VerifyPassphrase(store, []byte(verifyTestPassphrase)) {
		t.Fatal("the vault's passphrase was refused")
	}
	if keys, err := agent.List(); err != nil || len(keys) != 0 {
		t.Fatalf("List after verifying = %v, %v; want a locked agent's empty list", keys, err)
	}
	if err := agent.Lock(nil); err == nil {
		t.Error("Lock succeeded, so verifying the passphrase had unlocked the agent")
	}
	if err := agent.Unlock([]byte(verifyTestPassphrase)); err != nil {
		t.Errorf("Unlock after verifying: %v", err)
	}
}
