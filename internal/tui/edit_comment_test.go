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
	"strings"
	"testing"
	"time"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/keys"
	"github.com/ollykeran/sshush/internal/vault"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func unixSocketTempDirTUI(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "sshush-tui-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// writeTestKeyFile writes an unencrypted ed25519 key pair with the given comment.
func writeTestKeyFile(t *testing.T, dir, name, comment string) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".pub", []byte(keys.FormatPublicKey(signer, comment)), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// startAgentSocket serves an ExtendedAgent over a unix socket.
func startAgentSocket(t *testing.T, ext sshagent.ExtendedAgent) string {
	t.Helper()
	dir := unixSocketTempDirTUI(t)
	socketPath := filepath.Join(dir, "agent.sock")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
		os.Remove(socketPath)
	})
	go func() {
		_ = agent.ListenAndServe(ctx, socketPath, ext)
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
		t.Fatalf("dial socket: %v", err)
	}
	conn.Close()
	return socketPath
}

// startKeyringAgent starts an agent backed by an in-memory keyring holding one
// ed25519 key with the given comment, and returns its socket and fingerprint.
func startKeyringAgent(t *testing.T, keyPath, comment string) (socketPath, fingerprint string) {
	t.Helper()
	parsed, err := ssh.ParseRawPrivateKey(mustReadFile(t, keyPath))
	if err != nil {
		t.Fatal(err)
	}
	keyring := sshagent.NewKeyring().(sshagent.ExtendedAgent)
	if err := keyring.Add(sshagent.AddedKey{PrivateKey: parsed, Comment: comment}); err != nil {
		t.Fatal(err)
	}
	socketPath = startAgentSocket(t, keyring)
	pubKey, _, _, err := agent.ParseKeyFromPath(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	return socketPath, ssh.FingerprintSHA256(pubKey)
}

// startVaultAgent starts an agent backed by a vault holding one ed25519 key with
// the given comment, and returns its socket, the store, and the fingerprint.
func startVaultAgent(t *testing.T, keyPath, comment string) (socketPath string, store *vault.VaultStore, fingerprint string) {
	t.Helper()
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	var err error
	store, err = vault.Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("tui-test-passphrase")
	if err := vault.Init(store, passphrase); err != nil {
		t.Fatal(err)
	}
	va := vault.NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}
	parsed, err := ssh.ParseRawPrivateKey(mustReadFile(t, keyPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := va.Add(sshagent.AddedKey{PrivateKey: parsed, Comment: comment}); err != nil {
		t.Fatal(err)
	}
	socketPath = startAgentSocket(t, va)
	pubKey, _, _, err := agent.ParseKeyFromPath(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	return socketPath, store, ssh.FingerprintSHA256(pubKey)
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func agentClient(t *testing.T, socketPath string) sshagent.Agent {
	t.Helper()
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return sshagent.NewClient(conn)
}

func TestEditSaveAgentKeyCmd_keysMode(t *testing.T) {
	dir := t.TempDir()
	keyPath := writeTestKeyFile(t, dir, "id_ed25519", "old-comment")
	socketPath, fp := startKeyringAgent(t, keyPath, "old-comment")

	msg := editSaveAgentKeyCmd(socketPath, []string{keyPath}, fp, "new-comment")()
	em, ok := msg.(editSaveMsg)
	if !ok {
		t.Fatalf("want editSaveMsg, got %T", msg)
	}
	if em.err != nil {
		t.Fatalf("save: %v", em.err)
	}

	if _, comment, _, err := agent.ParseKeyFromPath(keyPath); err != nil {
		t.Fatal(err)
	} else if comment != "new-comment" {
		t.Errorf("key file comment: want %q, got %q", "new-comment", comment)
	}
	if pubData, err := os.ReadFile(keyPath + ".pub"); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(string(pubData), "new-comment") {
		t.Errorf(".pub should contain new comment; got: %s", pubData)
	}

	listed, err := agentClient(t, socketPath).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Comment != "new-comment" {
		t.Errorf("agent comment: want %q, got %q", "new-comment", listed[0].Comment)
	}
}

func TestEditSaveAgentKeyCmd_vaultMode(t *testing.T) {
	dir := t.TempDir()
	keyPath := writeTestKeyFile(t, dir, "id_ed25519", "old-comment")
	socketPath, store, fp := startVaultAgent(t, keyPath, "old-comment")

	msg := editSaveAgentKeyCmd(socketPath, []string{keyPath}, fp, "renamed-comment")()
	em, ok := msg.(editSaveMsg)
	if !ok {
		t.Fatalf("want editSaveMsg, got %T", msg)
	}
	if em.err != nil {
		t.Fatalf("save: %v", em.err)
	}

	if _, comment, _, err := agent.ParseKeyFromPath(keyPath); err != nil {
		t.Fatal(err)
	} else if comment != "renamed-comment" {
		t.Errorf("key file comment: want %q, got %q", "renamed-comment", comment)
	}

	ids := store.AllIdentities()
	if len(ids) != 1 || ids[0].Comment != "renamed-comment" {
		t.Errorf("vault identity comment: want %q, got %+v", "renamed-comment", ids)
	}

	listed, err := agentClient(t, socketPath).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Comment != "renamed-comment" {
		t.Errorf("agent comment: want %q, got %q", "renamed-comment", listed[0].Comment)
	}
}

func TestEditSaveAgentKeyCmd_vaultMode_noSourceFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := writeTestKeyFile(t, dir, "id_ed25519", "old-comment")
	// The key lives only in the vault; the config does not list its source file.
	socketPath, store, fp := startVaultAgent(t, keyPath, "old-comment")

	msg := editSaveAgentKeyCmd(socketPath, nil, fp, "renamed-comment")()
	em, ok := msg.(editSaveMsg)
	if !ok {
		t.Fatalf("want editSaveMsg, got %T", msg)
	}
	if em.err != nil {
		t.Fatalf("save: %v", em.err)
	}

	ids := store.AllIdentities()
	if len(ids) != 1 || ids[0].Comment != "renamed-comment" {
		t.Errorf("vault identity comment: want %q, got %+v", "renamed-comment", ids)
	}
	listed, err := agentClient(t, socketPath).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Comment != "renamed-comment" {
		t.Errorf("agent comment: want %q, got %q", "renamed-comment", listed[0].Comment)
	}
}

func TestEditSaveAgentKeyCmd_emptyComment(t *testing.T) {
	msg := editSaveAgentKeyCmd("", nil, "SHA256:whatever", "   ")()
	em, ok := msg.(editSaveMsg)
	if !ok {
		t.Fatalf("want editSaveMsg, got %T", msg)
	}
	if em.err == nil {
		t.Fatal("want error for empty comment")
	}
}

func TestEditSaveAgentKeyCmd_agentNotRunning(t *testing.T) {
	msg := editSaveAgentKeyCmd(filepath.Join(t.TempDir(), "missing.sock"), nil, "SHA256:whatever", "new-comment")()
	em, ok := msg.(editSaveMsg)
	if !ok {
		t.Fatalf("want editSaveMsg, got %T", msg)
	}
	if em.err == nil {
		t.Fatal("want error when agent is not running")
	}
}

func TestEditSaveAgentKeyCmd_unknownSourceFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := writeTestKeyFile(t, dir, "id_ed25519", "old-comment")
	socketPath, fp := startKeyringAgent(t, keyPath, "old-comment")

	// Key is loaded in the agent but no source file is resolvable.
	msg := editSaveAgentKeyCmd(socketPath, nil, fp, "new-comment")()
	em, ok := msg.(editSaveMsg)
	if !ok {
		t.Fatalf("want editSaveMsg, got %T", msg)
	}
	if em.err == nil {
		t.Fatal("want error when source file path cannot be resolved")
	}
	if !strings.Contains(em.err.Error(), "key_paths") {
		t.Errorf("error should mention key_paths; got: %v", em.err)
	}
}

func TestEditLoadAgentKeyCmd_resolvesSourceFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := writeTestKeyFile(t, dir, "id_ed25519", "file-comment")
	pub, _, _, err := agent.ParseKeyFromPath(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	// The agent reports a different comment than the file.
	msg := editLoadAgentKeyCmd([]string{keyPath}, &sshagent.Key{Blob: pub.Marshal(), Comment: "agent-comment"})()
	em, ok := msg.(editKeyLoadedMsg)
	if !ok {
		t.Fatalf("want editKeyLoadedMsg, got %T", msg)
	}
	if em.err != nil {
		t.Fatalf("load: %v", em.err)
	}
	if em.comment != "agent-comment" {
		t.Errorf("comment: want agent-reported %q, got %q", "agent-comment", em.comment)
	}
	if em.filePath != keyPath {
		t.Errorf("filePath: want %q, got %q", keyPath, em.filePath)
	}
	if em.rawKey == nil {
		t.Error("rawKey should be loaded from the resolved source file")
	}
	if em.fingerprint != ssh.FingerprintSHA256(pub) {
		t.Error("fingerprint mismatch")
	}
}

func TestEditLoadAgentKeyCmd_noSourceFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := writeTestKeyFile(t, dir, "id_ed25519", "file-comment")
	pub, _, _, err := agent.ParseKeyFromPath(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	msg := editLoadAgentKeyCmd(nil, &sshagent.Key{Blob: pub.Marshal(), Comment: "agent-comment"})()
	em, ok := msg.(editKeyLoadedMsg)
	if !ok {
		t.Fatalf("want editKeyLoadedMsg, got %T", msg)
	}
	if em.err != nil {
		t.Fatalf("load: %v", em.err)
	}
	if em.comment != "agent-comment" {
		t.Errorf("comment: want %q, got %q", "agent-comment", em.comment)
	}
	if em.filePath != "" {
		t.Errorf("filePath: want empty, got %q", em.filePath)
	}
	if em.rawKey != nil {
		t.Error("rawKey should be nil when no source file is resolvable")
	}
	if em.fingerprint == "" {
		t.Error("fingerprint should be set")
	}
	if !strings.Contains(em.pubKeyStr, "agent-comment") {
		t.Errorf("pubKeyStr should include the comment; got: %q", em.pubKeyStr)
	}
}
