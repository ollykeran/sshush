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

func TestEditAgentKeyCommentCmd_keysMode(t *testing.T) {
	dir := t.TempDir()
	keyPath := writeTestKeyFile(t, dir, "id_ed25519", "old-comment")

	parsed, err := ssh.ParseRawPrivateKey(mustReadFile(t, keyPath))
	if err != nil {
		t.Fatal(err)
	}
	keyring := sshagent.NewKeyring().(sshagent.ExtendedAgent)
	if err := keyring.Add(sshagent.AddedKey{PrivateKey: parsed, Comment: "old-comment"}); err != nil {
		t.Fatal(err)
	}
	socketPath := startAgentSocket(t, keyring)

	pubKey, _, _, err := agent.ParseKeyFromPath(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	fp := ssh.FingerprintSHA256(pubKey)

	msg := editAgentKeyCommentCmd(socketPath, []string{keyPath}, "keys", fp, "new-comment")()
	em, ok := msg.(agentEditCommentMsg)
	if !ok {
		t.Fatalf("want agentEditCommentMsg, got %T", msg)
	}
	if em.err != nil {
		t.Fatalf("edit comment: %v", em.err)
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

	client := agentClient(t, socketPath)
	listed, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Comment != "new-comment" {
		t.Errorf("agent comment: want %q, got %q", "new-comment", listed[0].Comment)
	}
}

func TestEditAgentKeyCommentCmd_vaultMode(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := vault.Open(vaultPath)
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

	keyPath := writeTestKeyFile(t, dir, "id_ed25519", "old-comment")
	parsed, err := ssh.ParseRawPrivateKey(mustReadFile(t, keyPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := va.Add(sshagent.AddedKey{PrivateKey: parsed, Comment: "old-comment"}); err != nil {
		t.Fatal(err)
	}
	socketPath := startAgentSocket(t, va)

	pubKey, _, _, err := agent.ParseKeyFromPath(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	fp := ssh.FingerprintSHA256(pubKey)

	msg := editAgentKeyCommentCmd(socketPath, []string{keyPath}, "vault", fp, "renamed-comment")()
	em, ok := msg.(agentEditCommentMsg)
	if !ok {
		t.Fatalf("want agentEditCommentMsg, got %T", msg)
	}
	if em.err != nil {
		t.Fatalf("edit comment: %v", em.err)
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

	client := agentClient(t, socketPath)
	listed, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Comment != "renamed-comment" {
		t.Errorf("agent comment: want %q, got %q", "renamed-comment", listed[0].Comment)
	}
}

func TestEditAgentKeyCommentCmd_emptyComment(t *testing.T) {
	msg := editAgentKeyCommentCmd("", nil, "keys", "SHA256:whatever", "   ")()
	em, ok := msg.(agentEditCommentMsg)
	if !ok {
		t.Fatalf("want agentEditCommentMsg, got %T", msg)
	}
	if em.err == nil {
		t.Fatal("want error for empty comment")
	}
}

func TestEditAgentKeyCommentCmd_unknownSourceFile(t *testing.T) {
	msg := editAgentKeyCommentCmd("", nil, "keys", "SHA256:unknown", "new-comment")()
	em, ok := msg.(agentEditCommentMsg)
	if !ok {
		t.Fatalf("want agentEditCommentMsg, got %T", msg)
	}
	if em.err == nil {
		t.Fatal("want error when source file cannot be resolved")
	}
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
