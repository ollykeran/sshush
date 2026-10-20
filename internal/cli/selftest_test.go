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
	t.Parallel()
	socketPath, agentClient := startTestAgent(t)

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	if err := runCreate("ed25519", 0, "test-agent-key", keyPath, false); err != nil {
		t.Fatal(err)
	}
	if err := agent.AddKeyFromPath(agentClient, keyPath); err != nil {
		t.Fatal(err)
	}

	cmd := newConfiguredCommand(&config.Config{SocketPath: socketPath})
	if err := runSelftest(cmd, nil); err != nil {
		t.Fatalf("runSelftest: %v", err)
	}
}

func TestRunSelftest_socketMissing(t *testing.T) {
	t.Parallel()
	cmd := newConfiguredCommand(&config.Config{SocketPath: filepath.Join(t.TempDir(), "nonexistent.sock")})
	if err := runSelftest(cmd, nil); err != nil {
		t.Fatalf("runSelftest with missing socket: %v", err)
	}
}

func TestRunSelftest_noKeysLoaded(t *testing.T) {
	t.Parallel()
	socketPath, _ := startTestAgent(t)

	cmd := newConfiguredCommand(&config.Config{SocketPath: socketPath})
	if err := runSelftest(cmd, nil); err != nil {
		t.Fatalf("runSelftest with empty agent: %v", err)
	}
}

func TestRunSelftest_multipleKeys(t *testing.T) {
	t.Parallel()
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

	cmd := newConfiguredCommand(&config.Config{SocketPath: socketPath})
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
	t.Parallel()
	cmd := &cobra.Command{Use: "selftest"}
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
	// Cannot use t.Parallel() with t.Setenv
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

	cmd := newConfiguredCommand(&config.Config{SocketPath: socketPath})
	if err := runSelftest(cmd, nil); err != nil {
		t.Fatalf("runSelftest: %v", err)
	}
}

func TestRunSelftest_sshAuthSockMismatch(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv
	socketPath, _ := startTestAgent(t)
	t.Setenv("SSH_AUTH_SOCK", "/tmp/different_agent.sock")

	cmd := newConfiguredCommand(&config.Config{SocketPath: socketPath})
	// Should still succeed — mismatch is a warning, not an error.
	if err := runSelftest(cmd, nil); err != nil {
		t.Fatalf("runSelftest with mismatched SSH_AUTH_SOCK: %v", err)
	}
}

func TestRunSelftest_sshAuthSockUnset(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv
	socketPath, _ := startTestAgent(t)
	t.Setenv("SSH_AUTH_SOCK", "") // restores the original value after the test
	os.Unsetenv("SSH_AUTH_SOCK")

	cmd := newConfiguredCommand(&config.Config{SocketPath: socketPath})
	// Should still succeed — unset is a warning, not an error.
	if err := runSelftest(cmd, nil); err != nil {
		t.Fatalf("runSelftest with unset SSH_AUTH_SOCK: %v", err)
	}
}
