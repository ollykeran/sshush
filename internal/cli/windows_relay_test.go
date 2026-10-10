package cli

import (
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/vault"
)

// startSilentRelay listens on a socket and hangs up on every connection
// without a word, which is what the relay in WSL does while the agent on
// Windows is not running.
func startSilentRelay(t *testing.T) string {
	t.Helper()
	socketPath := filepath.Join(unixSocketTempDir(t), "relay.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return socketPath
}

// TestWindowsRelay_agentDownIsNamed: the relay accepts every connection, so an
// agent that is down on Windows shows up as a connection that goes quiet. The
// commands must say which side to look at rather than report an EOF.
func TestWindowsRelay_agentDownIsNamed(t *testing.T) {
	socketPath := startSilentRelay(t)
	cfg := &config.Config{SocketPath: socketPath, AgentType: config.AgentTypeWindows}

	for name, run := range map[string]func() error{
		"list":   func() error { return runList(newConfiguredCommand(cfg), nil) },
		"lock":   func() error { return runLock(newConfiguredCommand(cfg), nil) },
		"unlock": func() error { return runUnlock(newConfiguredCommand(cfg), nil) },
	} {
		err := run()
		if err == nil || !strings.Contains(err.Error(), "agent on Windows is not answering") {
			t.Errorf("%s: want it to say the agent on Windows is not answering, got: %v", name, err)
		}
	}

	// A vault command says what it needs, and that 'sshush start' provides it.
	cmd := newVaultAddCommand()
	withConfig(cmd, cfg)
	keyPath := writeTestKey(t, t.TempDir(), "id_ed25519", "never-added")
	err := runVaultAdd(cmd, []string{keyPath})
	if err == nil || !strings.Contains(err.Error(), "requires a running vault agent") {
		t.Errorf("vault add: want it to ask for a running vault agent, got: %v", err)
	}
}

// TestWindowsRelay_reloadLeavesTheAgentsKeysAlone is the reload guard again,
// for the other kind of agent this config does not own: its keys are the
// Windows agent's, and key_paths here says nothing about them.
func TestWindowsRelay_reloadLeavesTheAgentsKeysAlone(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	socketPath, store, _ := startTestVaultAgent(t, []byte("reload-relay-pass"))
	keyPath := writeTestKey(t, t.TempDir(), "id_ed25519", "in-the-windows-vault")
	session, err := agent.Open(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := vault.AddPrivateKeyFile(session, keyPath, true); err != nil {
		t.Fatalf("seed identity: %v", err)
	}

	// The test agent stands in for a relay that is already up with an agent
	// answering behind it, so reload has nothing to start.
	configPath := writeConfigFile(t, "[agent]\ntype = \"windows\"\nsocket_path = \""+filepath.ToSlash(socketPath)+"\"\n")
	cfg, err := LoadMergedConfig(configPath, LoadOverrides{})
	if err != nil {
		t.Fatalf("LoadMergedConfig: %v", err)
	}
	cmd := cmdWithConfigFlag(configPath)
	withConfig(cmd, &cfg)

	if err := runReload(cmd, nil); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if left := store.AllIdentities(); len(left) != 1 {
		t.Fatalf("identities left in the vault: want 1, got %d", len(left))
	}
}

// TestWindowsRelay_startLeavesALockedVaultAloneWhenNothingWasStarted: a new
// shell that finds the relay and the agent both running must not prompt. Here
// stdin is not a terminal, so a prompt would show up as an attempt to read it.
func TestWindowsRelay_startLeavesALockedVaultAloneWhenNothingWasStarted(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	socketPath, _, _ := startTestVaultAgent(t, []byte("start-relay-pass"))
	session, err := agent.Open(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.Lock(nil); err != nil {
		t.Fatalf("lock: %v", err)
	}

	configPath := writeConfigFile(t, "[agent]\ntype = \"windows\"\nsocket_path = \""+filepath.ToSlash(socketPath)+"\"\n")
	cfg, err := LoadMergedConfig(configPath, LoadOverrides{})
	if err != nil {
		t.Fatalf("LoadMergedConfig: %v", err)
	}
	cmd := cmdWithConfigFlag(configPath)
	withConfig(cmd, &cfg)

	if err := runStartDaemon(cmd); err != nil {
		t.Fatalf("start: %v", err)
	}
	backend, err := session.Backend()
	if err != nil {
		t.Fatalf("backend: %v", err)
	}
	if !backend.VaultLocked {
		t.Fatal("the vault was unlocked by a start that started nothing")
	}
}
