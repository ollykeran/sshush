package cli

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/vault"
	"github.com/spf13/cobra"
	sshagent "golang.org/x/crypto/ssh/agent"
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

// firstBytes hands back what was read off a connection to find out whether the
// client had anything to say, ahead of the rest of the connection.
type firstBytes struct {
	net.Conn
	first []byte
}

func (c *firstBytes) Read(p []byte) (int, error) {
	if len(c.first) > 0 {
		n := copy(p, c.first)
		c.first = c.first[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}

// startCountingAgent serves keyring on a socket and counts the connections
// that send anything. That is the count that matters for an agent reached
// through the relay: each such connection is a Windows process started, and a
// connection that only checks the socket is there is not.
func startCountingAgent(t *testing.T, keyring sshagent.Agent) (socketPath string, speaking func() int) {
	t.Helper()
	socketPath = filepath.Join(unixSocketTempDir(t), "agent.sock")
	var count atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
	})
	ready := make(chan struct{})
	go func() {
		_ = agent.ListenAndHandle(ctx, socketPath, func(conn net.Conn) {
			defer conn.Close()
			first := make([]byte, 1)
			n, _ := conn.Read(first)
			if n == 0 {
				return
			}
			count.Add(1)
			_ = sshagent.ServeAgent(keyring, &firstBytes{Conn: conn, first: first[:n]})
		}, agent.WithReady(func() { close(ready) }))
	}()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("counting agent did not become ready")
	}
	return socketPath, func() int { return int(count.Load()) }
}

// windowsRelayCommand is a command carrying a "windows" config whose socket
// is socketPath, as the root pre-run would leave it.
func windowsRelayCommand(t *testing.T, socketPath string) *cobra.Command {
	t.Helper()
	configPath := writeConfigFile(t, "[agent]\ntype = \"windows\"\nsocket_path = \""+filepath.ToSlash(socketPath)+"\"\n")
	cfg, err := LoadMergedConfig(configPath, LoadOverrides{})
	if err != nil {
		t.Fatalf("LoadMergedConfig: %v", err)
	}
	cmd := cmdWithConfigFlag(configPath)
	withConfig(cmd, &cfg)
	return cmd
}

// TestWindowsRelay_startUsesOneConnection: start runs in every new shell, and
// through the relay each connection that speaks is a Windows process. Finding
// out that an agent is there, what kind it is, and what keys it holds must not
// take a connection each.
func TestWindowsRelay_startUsesOneConnection(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	t.Run("keys loaded", func(t *testing.T) {
		keyring := sshagent.NewKeyring()
		socketPath, speaking := startCountingAgent(t, keyring)
		keyPath := writeTestKey(t, t.TempDir(), "id_ed25519", "already-loaded")
		if err := agent.AddKeyFromPath(keyring, keyPath); err != nil {
			t.Fatalf("seed key: %v", err)
		}

		if err := runStartDaemon(windowsRelayCommand(t, socketPath)); err != nil {
			t.Fatalf("start: %v", err)
		}
		if got := speaking(); got != 1 {
			t.Fatalf("connections that spoke: want 1, got %d", got)
		}
	})

	// With no keys to show, start asks once more what the agent is, to say
	// whether that is an empty agent or a locked vault. Same connection.
	t.Run("locked vault", func(t *testing.T) {
		store, err := vault.Open(filepath.Join(t.TempDir(), "vault.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := vault.Init(store, []byte("one-connection-pass")); err != nil {
			t.Fatal(err)
		}
		socketPath, speaking := startCountingAgent(t, vault.NewVaultAgent(store))

		if err := runStartDaemon(windowsRelayCommand(t, socketPath)); err != nil {
			t.Fatalf("start: %v", err)
		}
		if got := speaking(); got != 1 {
			t.Fatalf("connections that spoke: want 1, got %d", got)
		}
	})
}

// TestCommandPrintsAgentMode: only the commands that go through start, and
// only for a relayed agent, take the "Agent:" line off the pre-run's hands.
// Everything else still has it printed for it.
func TestCommandPrintsAgentMode(t *testing.T) {
	t.Parallel()
	root := NewRootCommand()
	registerCommands(root)
	find := func(name string) *cobra.Command {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatalf("find %s: %v", name, err)
		}
		return cmd
	}
	relayed := &config.Config{AgentType: config.AgentTypeWindows}

	for name, cmd := range map[string]*cobra.Command{"sshush": root, "start": find("start"), "reload": find("reload")} {
		if !commandPrintsAgentMode(relayed, cmd) {
			t.Errorf("%s with a relayed agent: want it to print the line itself", name)
		}
	}
	for _, name := range []string{"list", "add", "lock", "stop"} {
		if commandPrintsAgentMode(relayed, find(name)) {
			t.Errorf("%s with a relayed agent: want the line printed for it", name)
		}
	}
	for _, agentType := range []string{config.AgentTypeKeys, config.AgentTypeVault, config.AgentTypeExternal} {
		if commandPrintsAgentMode(&config.Config{AgentType: agentType}, find("start")) {
			t.Errorf("start with a %q agent: want the line printed for it", agentType)
		}
	}
	if commandPrintsAgentMode(nil, find("start")) {
		t.Error("start with no config: want false")
	}
}
