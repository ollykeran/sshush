package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/spf13/cobra"
)

func TestRunSelftest_allChecksPass(t *testing.T) {
	socketPath, agentClient := startTestAgent(t)

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	if err := runCreate("ed25519", 0, "test-agent-key", keyPath, false); err != nil {
		t.Fatal(err)
	}
	if err := agent.AddKeyFromPath(agentClient, keyPath); err != nil {
		t.Fatal(err)
	}

	env.Config = &config.Config{SocketPath: socketPath}
	defer func() { env.Config = nil }()

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := runSelftest(cmd, nil); err != nil {
		t.Fatalf("runSelftest: %v", err)
	}
}

func TestRunSelftest_socketMissing(t *testing.T) {
	env.Config = &config.Config{SocketPath: "/tmp/nonexistent_sshush_test.sock"}
	defer func() { env.Config = nil }()

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := runSelftest(cmd, nil); err != nil {
		t.Fatalf("runSelftest with missing socket: %v", err)
	}
}

func TestRunSelftest_noKeysLoaded(t *testing.T) {
	socketPath, _ := startTestAgent(t)

	env.Config = &config.Config{SocketPath: socketPath}
	defer func() { env.Config = nil }()

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := runSelftest(cmd, nil); err != nil {
		t.Fatalf("runSelftest with empty agent: %v", err)
	}
}

func TestRunSelftest_multipleKeys(t *testing.T) {
	socketPath, agentClient := startTestAgent(t)

	dir := t.TempDir()
	for _, kt := range []string{"ed25519", "rsa", "ecdsa"} {
		keyPath := filepath.Join(dir, "id_"+kt)
		bits := 0
		if kt == "rsa" {
			bits = 2048
		} else if kt == "ecdsa" {
			bits = 256
		}
		if err := runCreate(kt, bits, "key-"+kt, keyPath, false); err != nil {
			t.Fatalf("create %s: %v", kt, err)
		}
		if err := agent.AddKeyFromPath(agentClient, keyPath); err != nil {
			t.Fatalf("add %s: %v", kt, err)
		}
	}

	env.Config = &config.Config{SocketPath: socketPath}
	defer func() { env.Config = nil }()

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := runSelftest(cmd, nil); err != nil {
		t.Fatalf("runSelftest with multiple keys: %v", err)
	}
}

func TestSelftestCommand_rejectsArgs(t *testing.T) {
	t.Parallel()
	cmd := newSelftestCommand()
	cmd.SetArgs([]string{"extra-arg"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for extra args")
	}
	if !strings.Contains(err.Error(), "selftest takes no arguments") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSelftestCommand_noConfig(t *testing.T) {
	// Not parallel — modifies env.Config.
	orig := env.Config
	env.Config = nil
	t.Cleanup(func() { env.Config = orig })

	cmd := &cobra.Command{Use: "selftest"}
	cmd.SetArgs([]string{})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := runSelftest(cmd, nil)
	if err == nil {
		t.Fatal("expected error when config not loaded")
	}
	if !strings.Contains(err.Error(), "config not loaded") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunSelftest_sshAuthSockMatches(t *testing.T) {
	socketPath, agentClient := startTestAgent(t)
	t.Setenv("SSH_AUTH_SOCK", socketPath)

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	if err := runCreate("ed25519", 0, "env-match", keyPath, false); err != nil {
		t.Fatal(err)
	}
	if err := agent.AddKeyFromPath(agentClient, keyPath); err != nil {
		t.Fatal(err)
	}

	env.Config = &config.Config{SocketPath: socketPath}
	defer func() { env.Config = nil }()

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := runSelftest(cmd, nil); err != nil {
		t.Fatalf("runSelftest: %v", err)
	}
}

func TestRunSelftest_sshAuthSockMismatch(t *testing.T) {
	socketPath, _ := startTestAgent(t)
	t.Setenv("SSH_AUTH_SOCK", "/tmp/different_agent.sock")

	env.Config = &config.Config{SocketPath: socketPath}
	defer func() { env.Config = nil }()

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	// Should still succeed — mismatch is a warning, not an error.
	if err := runSelftest(cmd, nil); err != nil {
		t.Fatalf("runSelftest with mismatched SSH_AUTH_SOCK: %v", err)
	}
}

func TestRunSelftest_sshAuthSockUnset(t *testing.T) {
	socketPath, _ := startTestAgent(t)
	os.Unsetenv("SSH_AUTH_SOCK")

	env.Config = &config.Config{SocketPath: socketPath}
	defer func() { env.Config = nil }()

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	// Should still succeed — unset is a warning, not an error.
	if err := runSelftest(cmd, nil); err != nil {
		t.Fatalf("runSelftest with unset SSH_AUTH_SOCK: %v", err)
	}
}
