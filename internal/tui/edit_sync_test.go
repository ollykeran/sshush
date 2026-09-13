package tui

import (
	"os"
	"testing"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/openssh"
	"github.com/ollykeran/sshush/internal/vault"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// TestEditSaveKeyCmd_ReloadsLoadedAgentKey is a regression test for the Edit
// tab silently leaving a running agent holding a stale comment after a
// rename. It must fail against the pre-fix editSaveKeyCmd, which only wrote
// the key file and never touched the agent.
func TestEditSaveKeyCmd_ReloadsLoadedAgentKey(t *testing.T) {
	dir := t.TempDir()
	privPath, fp := writeTUITestKey(t, dir, "id_ed25519", "before")

	socketPath, client := startTestKeyringAgent(t)
	keyData, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatal(err)
	}
	rawKey, err := ssh.ParseRawPrivateKey(keyData)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Add(sshagent.AddedKey{PrivateKey: rawKey, Comment: "before"}); err != nil {
		t.Fatal(err)
	}

	cmd := editSaveKeyCmd(rawKey, "after", privPath, fp, socketPath)
	msg := cmd()
	saveMsg, ok := msg.(editSaveMsg)
	if !ok {
		t.Fatalf("expected editSaveMsg, got %T", msg)
	}
	if saveMsg.err != nil {
		t.Fatalf("editSaveKeyCmd: %v", saveMsg.err)
	}

	data, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := openssh.ParsePrivateKeyBlob(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Comment != "after" {
		t.Errorf("on-disk comment: got %q, want %q", parsed.Comment, "after")
	}

	keysAfter, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keysAfter) != 1 {
		t.Fatalf("expected 1 key in agent after reload, got %d", len(keysAfter))
	}
	if keysAfter[0].Comment != "after" {
		t.Errorf("agent comment after reload: got %q, want %q (agent left stale)", keysAfter[0].Comment, "after")
	}
}

// TestEditSaveKeyCmd_VaultBackend_PersistsToVault covers the same regression
// for a vault-backed agent: saving via the Edit tab must persist the new
// comment to the vault store, not just the key file on disk.
func TestEditSaveKeyCmd_VaultBackend_PersistsToVault(t *testing.T) {
	dir := t.TempDir()
	privPath, fp := writeTUITestKey(t, dir, "id_ed25519", "before-vault")

	socketPath, store := startTestVaultAgentTUI(t, []byte("tui-edit-tab-test"))
	session, err := agent.Open(socketPath)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	if err := vault.AddPrivateKeyFile(session, privPath, true); err != nil {
		t.Fatalf("add private key file: %v", err)
	}
	session.Close()

	keyData, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatal(err)
	}
	rawKey, err := ssh.ParseRawPrivateKey(keyData)
	if err != nil {
		t.Fatal(err)
	}

	cmd := editSaveKeyCmd(rawKey, "after-vault", privPath, fp, socketPath)
	msg := cmd()
	saveMsg, ok := msg.(editSaveMsg)
	if !ok {
		t.Fatalf("expected editSaveMsg, got %T", msg)
	}
	if saveMsg.err != nil {
		t.Fatalf("editSaveKeyCmd: %v", saveMsg.err)
	}

	ids := store.AllIdentities()
	if len(ids) != 1 {
		t.Fatalf("expected 1 identity in vault, got %d", len(ids))
	}
	if ids[0].Comment != "after-vault" {
		t.Errorf("vault comment: got %q, want %q (vault left stale)", ids[0].Comment, "after-vault")
	}
}

// TestEditSaveKeyCmd_NoRunningAgent ensures a save still succeeds when no
// agent is reachable at socketPath — an absent agent is not a failure.
func TestEditSaveKeyCmd_NoRunningAgent(t *testing.T) {
	dir := t.TempDir()
	privPath, fp := writeTUITestKey(t, dir, "id_ed25519", "before")

	keyData, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatal(err)
	}
	rawKey, err := ssh.ParseRawPrivateKey(keyData)
	if err != nil {
		t.Fatal(err)
	}

	cmd := editSaveKeyCmd(rawKey, "after", privPath, fp, dir+"/no-agent.sock")
	msg := cmd()
	saveMsg, ok := msg.(editSaveMsg)
	if !ok {
		t.Fatalf("expected editSaveMsg, got %T", msg)
	}
	if saveMsg.err != nil {
		t.Fatalf("editSaveKeyCmd: %v", saveMsg.err)
	}

	data, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := openssh.ParsePrivateKeyBlob(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Comment != "after" {
		t.Errorf("on-disk comment: got %q, want %q", parsed.Comment, "after")
	}
}
