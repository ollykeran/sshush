package vaultops

import (
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/agent"
)

func TestLock_wipesMasterKey(t *testing.T) {
	f := startVaultAgent(t, false)

	if err := Lock(f.silentEnv()); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if !backendOf(t, f).VaultLocked {
		t.Fatal("vault locked: want true after lock, got false")
	}
}

func TestUnlockPassphrase_succeeds(t *testing.T) {
	f := startVaultAgent(t, false)
	f.lock(t)

	if err := UnlockPassphrase(f.silentEnv(), f.pass); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if backendOf(t, f).VaultLocked {
		t.Fatal("vault locked: want false after unlock, got true")
	}
}

func TestUnlockPassphrase_wrongPassphrase(t *testing.T) {
	f := startVaultAgent(t, false)
	f.lock(t)

	err := UnlockPassphrase(f.silentEnv(), []byte("not-the-passphrase"))
	if got := CodeOf(err); got != CodeWrongPassphrase {
		t.Fatalf("code: want %v, got %v (err %v)", CodeWrongPassphrase, got, err)
	}
}

func TestUnlockPassphrase_alreadyUnlocked(t *testing.T) {
	f := startVaultAgent(t, false)

	err := UnlockPassphrase(f.silentEnv(), f.pass)
	if got := CodeOf(err); got != CodeNotLocked {
		t.Fatalf("code: want %v, got %v (err %v)", CodeNotLocked, got, err)
	}
}

func TestUnlockRecovery_succeeds(t *testing.T) {
	f := startVaultAgent(t, true)
	f.lock(t)

	if err := UnlockRecovery(f.silentEnv(), f.mnemonic); err != nil {
		t.Fatalf("unlock with recovery: %v", err)
	}
	if backendOf(t, f).VaultLocked {
		t.Fatal("vault locked: want false after recovery unlock, got true")
	}
}

func TestUnlockRecovery_noRecoveryEnabled(t *testing.T) {
	f := startVaultAgent(t, false)
	f.lock(t)

	err := UnlockRecovery(f.silentEnv(), "word word word")
	if got := CodeOf(err); got != CodeNoRecovery {
		t.Fatalf("code: want %v, got %v (err %v)", CodeNoRecovery, got, err)
	}
}

// TestUnlockRecovery_wrongPhrase pins what the agent actually reports today: a
// phrase that fails to decrypt the wrapped master key comes back as an
// unexplained internal failure, because VaultAgent.UnlockWithRecovery wraps the
// decrypt error instead of returning its wrong-passphrase sentinel. describe
// maps agent.ErrWrongPassphrase to the phrase-specific sentence for when that
// is fixed; until then the reason does not reach us.
func TestUnlockRecovery_wrongPhrase(t *testing.T) {
	f := startVaultAgent(t, true)
	f.lock(t)

	err := UnlockRecovery(f.silentEnv(), "abandon abandon abandon")
	if got := CodeOf(err); got != CodeAgentFailed {
		t.Fatalf("code: want %v, got %v (err %v)", CodeAgentFailed, got, err)
	}
	if !strings.Contains(err.Error(), "unlock-recovery failed") {
		t.Fatalf("message: want it to name the operation, got %q", err.Error())
	}
	if backendOf(t, f).VaultLocked != true {
		t.Fatal("vault locked: want it to stay locked after a bad phrase")
	}
}

func backendOf(t *testing.T, f *fixture) agent.Backend {
	t.Helper()
	session, err := agent.Open(f.socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	backend, err := session.Backend()
	if err != nil {
		t.Fatalf("probe backend: %v", err)
	}
	return backend
}
