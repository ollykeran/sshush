package tui

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/openssh"
	"github.com/ollykeran/sshush/internal/vault"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func unixSocketTempDirTUI(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "sshush-tui-a-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// startTestKeyringAgent starts an in-process keys-mode SSH agent on a temp unix socket.
func startTestKeyringAgent(t *testing.T) (socketPath string, client sshagent.ExtendedAgent) {
	t.Helper()
	dir := unixSocketTempDirTUI(t)
	socketPath = filepath.Join(dir, "agent.sock")

	keyring := sshagent.NewKeyring()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
	})
	go func() {
		_ = agent.ListenAndServe(ctx, socketPath, keyring.(sshagent.ExtendedAgent))
	}()

	var conn net.Conn
	var err error
	for i := 0; i < 50; i++ {
		conn, err = net.Dial("unix", socketPath)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial agent socket: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return socketPath, sshagent.NewClient(conn)
}

// startTestVaultAgentTUI starts an in-process vault-backed SSH agent, unlocked with
// passphrase. Returns the socket path and the vault store for assertions.
func startTestVaultAgentTUI(t *testing.T, passphrase []byte) (socketPath string, store *vault.VaultStore) {
	t.Helper()
	dir := unixSocketTempDirTUI(t)
	socketPath = filepath.Join(dir, "agent.sock")

	vaultPath := filepath.Join(dir, "vault.json")
	var err error
	store, err = vault.Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(store, passphrase); err != nil {
		t.Fatal(err)
	}
	va := vault.NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
	})
	go func() {
		_ = agent.ListenAndServe(ctx, socketPath, va)
	}()

	var conn net.Conn
	for i := 0; i < 50; i++ {
		conn, err = net.Dial("unix", socketPath)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial agent socket: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return socketPath, store
}

// writeTUITestKey generates an ed25519 key with the given comment and writes it to dir.
// Returns the private key path and its SHA256 fingerprint.
func writeTUITestKey(t *testing.T, dir, filename, comment string) (path, fingerprint string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		t.Fatal(err)
	}
	privPath := filepath.Join(dir, filename)
	if err := os.WriteFile(privPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return privPath, ssh.FingerprintSHA256(signer.PublicKey())
}

func TestCommentOverlay_OpenPrefillsCurrentComment(t *testing.T) {
	_, agentScreen := newAgentTestSkeleton()
	agentScreen.width = 80
	seedAgentKeyRows(agentScreen, 2)
	agentScreen.keyTable.Table.SetCursor(1)

	_, cmd := agentScreen.editSelectedKeyComment()
	if !agentScreen.commentOverlay.active {
		t.Fatal("expected comment overlay to be active")
	}
	if agentScreen.commentOverlay.fingerprint != "SHA256:fp1" {
		t.Errorf("fingerprint: got %q, want %q", agentScreen.commentOverlay.fingerprint, "SHA256:fp1")
	}
	if got := agentScreen.commentOverlay.commentIn.Value(); got != "key1" {
		t.Errorf("prefilled comment: got %q, want %q", got, "key1")
	}
	if cmd == nil {
		t.Error("expected a focus command from Show")
	}
}

func TestCommentOverlay_EscCancelsLeavesStateUnchanged(t *testing.T) {
	_, agentScreen := newAgentTestSkeleton()
	agentScreen.width = 80
	seedAgentKeyRows(agentScreen, 1)
	agentScreen.editSelectedKeyComment()

	agentScreen.commentOverlay.commentIn.SetValue("changed-but-not-saved")
	_, cmd := agentScreen.Update(tea.KeyPressMsg{Text: "esc", Code: tea.KeyEscape})
	if agentScreen.commentOverlay.active {
		t.Error("expected overlay to close on esc")
	}
	if cmd != nil {
		t.Error("expected no save command on esc")
	}
}

func TestAgentScreen_EditComment_SavesToFileAndReloadsAgent(t *testing.T) {
	// Cannot use t.Parallel() with a package-level fingerprint registry shared across tests.
	dir := t.TempDir()
	privPath, fp := writeTUITestKey(t, dir, "id_ed25519", "before")
	agent.RegisterFilepath(fp, privPath)

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

	_, agentScreen := newAgentTestSkeleton()
	agentScreen.socketPath = socketPath
	agentScreen.width = 80
	agentScreen.keyTable.SetRows([]table.Row{{"ssh-ed25519", fp, "before"}})
	agentScreen.keyTable.Table.SetCursor(0)

	agentScreen.editSelectedKeyComment()
	agentScreen.commentOverlay.commentIn.SetValue("after")
	_, cmd := agentScreen.Update(tea.KeyPressMsg{Text: "enter", Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a save command")
	}
	msg := cmd()
	model, _ := agentScreen.Update(msg)
	agentScreen = model.(*AgentScreen)

	if agentScreen.commentOverlay.active {
		t.Error("expected overlay to close after successful save")
	}
	if agentScreen.statusErr {
		t.Errorf("unexpected error status: %s", agentScreen.status)
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
		t.Errorf("agent comment after reload: got %q, want %q", keysAfter[0].Comment, "after")
	}
}

func TestCommentOverlay_UnknownFingerprintErrors(t *testing.T) {
	socketPath, _ := startTestKeyringAgent(t)
	cmd := saveCommentOverlayCmd(socketPath, "SHA256:unregistered-fingerprint", "new-comment")
	msg := cmd()
	saved, ok := msg.(commentOverlaySavedMsg)
	if !ok {
		t.Fatalf("expected commentOverlaySavedMsg, got %T", msg)
	}
	if saved.err == nil {
		t.Fatal("expected error for unregistered fingerprint")
	}
}

func TestCommentOverlay_VaultBackend_PersistsToVault(t *testing.T) {
	dir := t.TempDir()
	privPath, fp := writeTUITestKey(t, dir, "id_ed25519", "before-vault")
	agent.RegisterFilepath(fp, privPath)

	socketPath, store := startTestVaultAgentTUI(t, []byte("tui-overlay-test"))
	session, err := agent.Open(socketPath)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	defer session.Close()
	if err := vault.AddPrivateKeyFile(session, privPath, true); err != nil {
		t.Fatalf("add private key file: %v", err)
	}

	cmd := saveCommentOverlayCmd(socketPath, fp, "after-vault")
	msg := cmd()
	saved, ok := msg.(commentOverlaySavedMsg)
	if !ok {
		t.Fatalf("expected commentOverlaySavedMsg, got %T", msg)
	}
	if saved.err != nil {
		t.Fatalf("saveCommentOverlayCmd: %v", saved.err)
	}

	ids := store.AllIdentities()
	if len(ids) != 1 {
		t.Fatalf("expected 1 identity, got %d", len(ids))
	}
	if ids[0].Comment != "after-vault" {
		t.Errorf("vault comment: got %q, want %q", ids[0].Comment, "after-vault")
	}

	data, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := openssh.ParsePrivateKeyBlob(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Comment != "after-vault" {
		t.Errorf("on-disk comment: got %q, want %q", parsed.Comment, "after-vault")
	}
}
