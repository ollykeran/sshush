package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/spf13/cobra"
)

// writeConfigFile writes body to a temp config.toml and returns its path.
func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// cmdWithConfigFlag returns a bare cobra.Command with a --config flag set to path,
// matching the flag registered by newStartCommand/newReloadCommand.
func cmdWithConfigFlag(path string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().StringP("config", "c", "", "path to config file")
	_ = cmd.Flags().Set("config", path)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd
}

func TestExternalMode_StartErrorsWhenUnreachable(t *testing.T) {
	// runStartDaemon only needs the config file to exist so ResolveConfigPath
	// succeeds; the behavior itself is driven by env.Config.
	configPath := writeConfigFile(t, "[agent]\nsocket_path = \"/tmp/placeholder.sock\"\nkey_paths = []\n")
	cmd := cmdWithConfigFlag(configPath)

	unreachableSocket := filepath.Join(t.TempDir(), "no-agent-here.sock")
	orig := env.Config
	env.Config = &config.Config{SocketPath: unreachableSocket, AgentType: config.AgentTypeExternal}
	t.Cleanup(func() { env.Config = orig })

	pidDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", pidDir)

	err := runStartDaemon(cmd)
	if err == nil {
		t.Fatal("expected error when external agent is unreachable")
	}
	if !strings.Contains(err.Error(), "external") {
		t.Errorf("expected error to mention external mode, got: %v", err)
	}
	// sshush must not have spawned/managed anything: no pidfile written.
	entries, _ := os.ReadDir(pidDir)
	if len(entries) != 0 {
		t.Errorf("expected no pidfile written in external mode, found: %v", entries)
	}
}

func TestExternalMode_StopRefuses(t *testing.T) {
	orig := env.Config
	env.Config = &config.Config{AgentType: config.AgentTypeExternal}
	t.Cleanup(func() { env.Config = orig })

	cmd := &cobra.Command{}
	err := runStop(cmd, nil)
	if err == nil {
		t.Fatal("expected runStop to refuse in external mode")
	}
	if !strings.Contains(err.Error(), "external") {
		t.Errorf("expected error to mention external mode, got: %v", err)
	}
}

func TestExternalMode_ReloadErrorsOnRestartNeeded(t *testing.T) {
	// reload loads config fresh from disk (not env.Config), so the file itself
	// must declare external = true and point at an unreachable socket to force
	// the needsRestart branch.
	unreachableSocket := filepath.Join(t.TempDir(), "no-agent-here.sock")
	configPath := writeConfigFile(t, "[agent]\nsocket_path = \""+unreachableSocket+"\"\nkey_paths = []\ntype = \"external\"\n")
	cmd := cmdWithConfigFlag(configPath)

	// Ensure SSH_AUTH_SOCK fallback doesn't accidentally succeed.
	t.Setenv("SSH_AUTH_SOCK", "")

	err := runReload(cmd, nil)
	if err == nil {
		t.Fatal("expected runReload to error when external agent is unreachable")
	}
	if !strings.Contains(err.Error(), "external") {
		t.Errorf("expected error to mention external mode, got: %v", err)
	}
}

func TestLoadMergedConfig_externalFallsBackToSSHAuthSock(t *testing.T) {
	configPath := writeConfigFile(t, "[agent]\ntype = \"external\"\n")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/some-real-ssh-agent.sock")

	cfg, err := LoadMergedConfig(configPath, LoadOverrides{})
	if err != nil {
		t.Fatalf("LoadMergedConfig: %v", err)
	}
	if cfg.SocketPath != "/tmp/some-real-ssh-agent.sock" {
		t.Errorf("SocketPath: got %q, want SSH_AUTH_SOCK value", cfg.SocketPath)
	}
}

func TestLoadMergedConfig_externalWithExplicitSocketIgnoresSSHAuthSock(t *testing.T) {
	configPath := writeConfigFile(t, "[agent]\ntype = \"external\"\nsocket_path = \"/tmp/configured.sock\"\n")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/some-other-agent.sock")

	cfg, err := LoadMergedConfig(configPath, LoadOverrides{})
	if err != nil {
		t.Fatalf("LoadMergedConfig: %v", err)
	}
	if cfg.SocketPath != "/tmp/configured.sock" {
		t.Errorf("SocketPath: got %q, want the configured path (SSH_AUTH_SOCK should not override it)", cfg.SocketPath)
	}
}

func TestExternalMode_StartSucceedsViaSSHAuthSock(t *testing.T) {
	// No socket_path in config at all — mirrors a real ssh-agent whose socket
	// path changes every launch and is only known via SSH_AUTH_SOCK.
	socketPath, _ := startTestAgent(t)
	t.Setenv("SSH_AUTH_SOCK", socketPath)

	configPath := writeConfigFile(t, "[agent]\ntype = \"external\"\n")
	cfg, err := LoadMergedConfig(configPath, LoadOverrides{})
	if err != nil {
		t.Fatalf("LoadMergedConfig: %v", err)
	}
	if cfg.SocketPath != socketPath {
		t.Fatalf("SocketPath: got %q, want %q", cfg.SocketPath, socketPath)
	}

	orig := env.Config
	env.Config = &cfg
	t.Cleanup(func() { env.Config = orig })

	cmd := cmdWithConfigFlag(configPath)
	if err := runStartDaemon(cmd); err != nil {
		t.Fatalf("runStartDaemon: %v", err)
	}
}

func TestExternalMode_StartErrorsWhenNoSocketFoundAtAll(t *testing.T) {
	configPath := writeConfigFile(t, "[agent]\nsocket_path = \"/tmp/placeholder.sock\"\n")
	cmd := cmdWithConfigFlag(configPath)

	orig := env.Config
	env.Config = &config.Config{AgentType: config.AgentTypeExternal} // SocketPath left empty
	t.Cleanup(func() { env.Config = orig })

	pidDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", pidDir)

	err := runStartDaemon(cmd)
	if err == nil {
		t.Fatal("expected error when external mode has no socket at all")
	}
	if !strings.Contains(err.Error(), "no socket found") {
		t.Errorf("expected error to mention 'no socket found', got: %v", err)
	}
}

// An unreachable agent no longer stops the server from starting — it asks the
// agent per connection, so one that appears later is picked up. The hint printed
// alongside that warning still has to say whose agent it is waiting for.
func TestExternalMode_ServerHintMentionsExternal(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "no-agent-here.sock")
	cfg := config.Config{
		SocketPath:       socketPath,
		AgentType:        config.AgentTypeExternal,
		ServerListenPort: 2222,
	}
	hint := startAgentHint(cfg)
	if !strings.Contains(hint, "external") {
		t.Errorf("hint should mention external mode, got: %q", hint)
	}
	if !strings.Contains(hint, socketPath) {
		t.Errorf("hint should name the socket to start the agent at, got: %q", hint)
	}
	if strings.Contains(hint, "sshush start") {
		t.Errorf("hint should not offer to start somebody else's agent, got: %q", hint)
	}
}

func TestExternalMode_ServerHintWithNoSocketSaysSo(t *testing.T) {
	hint := startAgentHint(config.Config{AgentType: config.AgentTypeExternal, ServerListenPort: 2222})
	if !strings.Contains(hint, "no socket found") {
		t.Errorf("hint should mention that no socket was found, got: %q", hint)
	}
}

func TestServerHint_ForSSHushOwnAgentOffersStart(t *testing.T) {
	hint := startAgentHint(config.Config{ServerListenPort: 2222})
	if !strings.Contains(hint, "sshush start") {
		t.Errorf("hint should point at 'sshush start', got: %q", hint)
	}
}
