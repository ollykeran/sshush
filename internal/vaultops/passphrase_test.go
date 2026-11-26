package vaultops

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/vault"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func TestChangePassphrase_throughTheAgent(t *testing.T) {
	f := startVaultAgent(t, true)
	f.addKey(t, "id_a", "a@test", true)
	recoveryTxt := filepath.Join(f.dir, "recovery.txt")
	if err := os.WriteFile(recoveryTxt, []byte(f.mnemonic+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	newPass := []byte("brand-new-passphrase")

	if _, err := ChangePassphrase(f.env(), vault.Credential{Passphrase: []byte("not-it-at-all")}, newPass, true); CodeOf(err) != CodeWrongPassphrase {
		t.Fatalf("wrong passphrase: got %v", err)
	}
	if _, err := ChangePassphrase(f.env(), vault.Credential{Passphrase: f.pass}, []byte("short"), true); CodeOf(err) != CodeWeakPassphrase {
		t.Fatalf("weak passphrase: got %v", err)
	}

	res, err := ChangePassphrase(f.env(), vault.Credential{Passphrase: f.pass}, newPass, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Fields(res.Mnemonic)) != 24 || res.RecoveryFile != recoveryTxt {
		t.Errorf("result %+v", res)
	}
	if got, _ := os.ReadFile(recoveryTxt); string(got) != res.Mnemonic+"\n" {
		t.Error("recovery.txt does not hold the new phrase")
	}
	// The agent kept serving, unlocked, and the file opens with the new one.
	if l, err := List(f.env()); err != nil || len(l.Identities) != 1 || l.Identities[0].Loaded != LoadYes {
		t.Errorf("after the change: %+v, %v", l, err)
	}
	if _, err := CheckVaultFile(f.vaultPath, newPass); err != nil {
		t.Errorf("new passphrase on the file: %v", err)
	}

	// And with the new recovery phrase as the credential, dropping recovery.
	res, err = ChangePassphrase(f.env(), vault.Credential{Mnemonic: res.Mnemonic}, []byte("third-passphrase"), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mnemonic != "" || res.RecoveryFile != recoveryTxt {
		t.Errorf("result %+v", res)
	}
	if _, err := os.Stat(recoveryTxt); !os.IsNotExist(err) {
		t.Error("recovery.txt left behind with a phrase that opens nothing")
	}
	if l, _ := List(f.env()); l.Recovery != RecoveryOff {
		t.Errorf("recovery %v, want off", l.Recovery)
	}
}

func TestChangePassphrase_inTheFileWithNoAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	newVaultFile(t, path, []byte("old-passphrase"), false)
	env := Env{VaultPath: path, SocketPath: filepath.Join(unixSocketTempDirVaultOps(t), "none.sock")}
	res, err := ChangePassphrase(env, vault.Credential{Passphrase: []byte("old-passphrase")}, []byte("new-passphrase"), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.RecoveryFile != "" {
		t.Errorf("made a recovery.txt that was not there: %s", res.RecoveryFile)
	}
	if _, err := CheckVaultFile(path, []byte("new-passphrase")); err != nil {
		t.Error(err)
	}
	if sum, _ := CheckVaultFile(path, []byte("new-passphrase")); !sum.Recovery {
		t.Error("no recovery after asking for it")
	}
}

func TestRegenerateRecovery(t *testing.T) {
	f := startVaultAgent(t, false)
	if _, err := RegenerateRecovery(f.env(), vault.Credential{Mnemonic: strings.Repeat("abandon ", 23) + "art"}); CodeOf(err) != CodeNoRecovery {
		t.Fatalf("phrase on a vault without one: got %v", err)
	}
	res, err := RegenerateRecovery(f.env(), vault.Credential{Passphrase: f.pass})
	if err != nil {
		t.Fatal(err)
	}
	f.lock(t)
	if err := UnlockRecovery(f.env(), res.Mnemonic); err != nil {
		t.Errorf("new phrase does not unlock: %v", err)
	}
}

// olderVault is a vault agent from before the secrets could be changed.
type olderVault struct{ *vault.VaultAgent }

func (o olderVault) Extension(extensionType string, contents []byte) ([]byte, error) {
	if op, _, err := agent.DecodeOpRequest(contents); err == nil && extensionType == agent.ExtensionOp &&
		(op == agent.OpChangePassphrase || op == agent.OpSetRecovery) {
		return agent.EncodeOpResponse(agent.StatusUnsupportedOp, nil), nil
	}
	return o.VaultAgent.Extension(extensionType, contents)
}

func TestChangePassphrase_olderAgentIsNotGoneAround(t *testing.T) {
	f := startVaultAgentAs(t, false, func(va *vault.VaultAgent) sshagent.ExtendedAgent { return olderVault{va} })
	before, _ := os.ReadFile(f.vaultPath)
	_, err := ChangePassphrase(f.env(), vault.Credential{Passphrase: f.pass}, []byte("new-passphrase"), false)
	if CodeOf(err) != CodeAgentTooOld || !strings.Contains(HintOf(err), "reload") {
		t.Fatalf("got %v (hint %q)", err, HintOf(err))
	}
	if after, _ := os.ReadFile(f.vaultPath); !bytes.Equal(before, after) {
		t.Error("changed the file under the agent")
	}
}
