package cli

import (
	"net"
	"path/filepath"
	"testing"

	"github.com/ollykeran/sshush/internal/agent"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func TestAgentIntegration_CreateAddListRemove(t *testing.T) {
	socketPath, agentClient := startTestAgent(t)

	// 1. Create a key via CLI create logic
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	if err := runCreate("ed25519", 0, "agent-test-key", keyPath, false); err != nil {
		t.Fatalf("runCreate: %v", err)
	}

	// 2. Add key to agent
	if err := agent.AddKeyFromPath(agentClient, keyPath); err != nil {
		t.Fatalf("AddKeyFromPath: %v", err)
	}

	// 3. List keys from agent and verify
	keys, err := agentClient.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].Comment != "agent-test-key" {
		t.Errorf("comment: got %q, want %q", keys[0].Comment, "agent-test-key")
	}
	if keys[0].Type() != "ssh-ed25519" {
		t.Errorf("type: got %q, want %q", keys[0].Type(), "ssh-ed25519")
	}

	// 4. Sign data and verify
	data := []byte("test payload")
	sig, err := agentClient.Sign(keys[0], data)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := keys[0].Verify(data, sig); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	// 5. Remove key from agent
	if err := agentClient.Remove(keys[0]); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	keysAfter, err := agentClient.List()
	if err != nil {
		t.Fatalf("List after remove: %v", err)
	}
	if len(keysAfter) != 0 {
		t.Errorf("expected 0 keys after remove, got %d", len(keysAfter))
	}

	_ = socketPath
}

func TestAgentIntegration_MultipleKeys(t *testing.T) {
	_, agentClient := startTestAgent(t)
	dir := t.TempDir()

	keyTypes := []struct {
		name    string
		keyType string
		bits    int
	}{
		{"ed25519-key", "ed25519", 0},
		{"rsa-key", "rsa", 2048},
		{"ecdsa-key", "ecdsa", 256},
	}

	for _, kt := range keyTypes {
		path := filepath.Join(dir, "id_"+kt.keyType)
		if err := runCreate(kt.keyType, kt.bits, kt.name, path, false); err != nil {
			t.Fatalf("runCreate %s: %v", kt.keyType, err)
		}
		if err := agent.AddKeyFromPath(agentClient, path); err != nil {
			t.Fatalf("add %s: %v", kt.keyType, err)
		}
	}

	keys, err := agentClient.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}

	comments := map[string]bool{}
	for _, k := range keys {
		comments[k.Comment] = true
	}
	for _, kt := range keyTypes {
		if !comments[kt.name] {
			t.Errorf("missing key with comment %q", kt.name)
		}
	}
}

func TestAgentIntegration_EditThenAdd(t *testing.T) {
	_, agentClient := startTestAgent(t)
	dir := t.TempDir()

	keyPath := filepath.Join(dir, "id_ed25519")
	if err := runCreate("ed25519", 0, "before-edit", keyPath, false); err != nil {
		t.Fatalf("runCreate: %v", err)
	}

	if err := runEdit(keyPath, "", "after-edit", false, ""); err != nil {
		t.Fatalf("runEdit: %v", err)
	}

	if err := agent.AddKeyFromPath(agentClient, keyPath); err != nil {
		t.Fatalf("add: %v", err)
	}
	keys, err := agentClient.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].Comment != "after-edit" {
		t.Errorf("comment: got %q, want %q", keys[0].Comment, "after-edit")
	}
}

func TestAgentIntegration_ExportMatchesAgent(t *testing.T) {
	_, agentClient := startTestAgent(t)
	dir := t.TempDir()

	keyPath := filepath.Join(dir, "id_ed25519")
	if err := runCreate("ed25519", 0, "export-check", keyPath, false); err != nil {
		t.Fatalf("runCreate: %v", err)
	}

	if err := agent.AddKeyFromPath(agentClient, keyPath); err != nil {
		t.Fatalf("add: %v", err)
	}

	keys, err := agentClient.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	agentFP := ssh.FingerprintSHA256(keys[0])

	// Parse the key file and compare fingerprints
	pubKey, _, _, err := agent.ParseKeyFromPath(keyPath)
	if err != nil {
		t.Fatalf("ParseKeyFromPath: %v", err)
	}
	fileFP := ssh.FingerprintSHA256(pubKey)

	if agentFP != fileFP {
		t.Errorf("fingerprint mismatch: agent=%s file=%s", agentFP, fileFP)
	}
}

func TestAgentIntegration_ConnectViaSocket(t *testing.T) {
	socketPath, _ := startTestAgent(t)
	dir := t.TempDir()

	keyPath := filepath.Join(dir, "id_ed25519")
	if err := runCreate("ed25519", 0, "socket-test", keyPath, false); err != nil {
		t.Fatalf("runCreate: %v", err)
	}

	// Connect fresh to the socket
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	freshClient := sshagent.NewClient(conn)
	if err := agent.AddKeyFromPath(freshClient, keyPath); err != nil {
		t.Fatalf("add: %v", err)
	}

	keys, err := freshClient.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}

	data := []byte("verify-socket-works")
	sig, err := freshClient.Sign(keys[0], data)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := keys[0].Verify(data, sig); err != nil {
		t.Fatalf("verify: %v", err)
	}
}
