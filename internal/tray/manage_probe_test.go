//go:build !windows

package tray

import (
	"crypto/ed25519"
	"path/filepath"
	"testing"

	"github.com/ollykeran/sshush/internal/vaultops"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func TestProbeMainListsTheAgentsVault(t *testing.T) {
	sock := serveVault(t)
	path, fp := writeKey(t, "probed", "")
	if _, err := AddKeys(sock, []string{path}, nil); err != nil {
		t.Fatal(err)
	}
	s := ProbeMain(vaultops.Env{SocketPath: sock})
	if !s.Reachable || !s.isVault() || s.Backend.VaultLocked || s.ListErr != nil {
		t.Fatalf("got %+v", s)
	}
	rows := s.Rows()
	if len(rows) != 1 || rows[0].Fingerprint != fp || rows[0].Comment != "probed" || rows[0].Loaded != vaultops.LoadYes {
		t.Errorf("rows: got %+v", rows)
	}
	if a := mainActions(s, &rows[0]); !a.Unload || !a.Remove || !a.Stop {
		t.Errorf("actions on a loaded identity: got %+v", a)
	}
}

func TestProbeMainKeyringListsItsKeys(t *testing.T) {
	kr := sshagent.NewKeyring()
	_, priv, _ := ed25519.GenerateKey(nil)
	if err := kr.Add(sshagent.AddedKey{PrivateKey: priv, Comment: "mem"}); err != nil {
		t.Fatal(err)
	}
	s := ProbeMain(vaultops.Env{SocketPath: serveKeyring(t, kr), VaultPath: filepath.Join(t.TempDir(), "vault.json")})
	if !s.Reachable || s.isVault() {
		t.Fatalf("got %+v", s)
	}
	if rows := s.Rows(); len(rows) != 1 || rows[0].Comment != "mem" {
		t.Errorf("rows: got %+v", rows)
	}
	// The vault file is read beside the agent, for the Vault page alone.
	if !s.VaultAside || !s.Listing || s.ListErr != nil || s.showsVault() || s.needsVault() {
		t.Errorf("vault beside a keyring: got %+v", s)
	}
	if va := vaultActions(s); !va.CreateVault || va.Unlock || va.Lock {
		t.Errorf("vault actions beside a keyring: got %+v", va)
	}
}

func TestProbeMainNoAgentReadsTheFile(t *testing.T) {
	vaultFile := filepath.Join(t.TempDir(), "vault.json")
	s := ProbeMain(vaultops.Env{SocketPath: filepath.Join(t.TempDir(), "none.sock"), VaultPath: vaultFile, AgentVaultPath: vaultFile})
	if s.Reachable || !s.Listing || s.ListErr != nil || s.Listed.Initialized {
		t.Errorf("got %+v", s)
	}
	// A vault agent will not start without its vault, so creating one comes first.
	if a := mainActions(s, nil); a.Start || a.Add || !a.CreateVault {
		t.Errorf("actions with no agent and no vault: got %+v", a)
	}
}
