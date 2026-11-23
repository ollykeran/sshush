package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The "windows" agent type: inside WSL, the daemon only relays socket_path to
// the agent of the sshush on the Windows side.

func writeAgentConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfig_windowsAgentType(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := writeAgentConfig(t, "[agent]\ntype = \"windows\"\nsocket_path = \"/tmp/relay.sock\"\nwindows_sshush = \"~/bin/sshush.exe\"\n")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.RelaysToWindows() {
		t.Fatalf("AgentType: got %q, want %q", cfg.AgentType, AgentTypeWindows)
	}
	if want := filepath.Join(home, "bin", "sshush.exe"); cfg.WindowsSSHush != want {
		t.Errorf("WindowsSSHush: got %q, want ~ expanded to %q", cfg.WindowsSSHush, want)
	}
	// Whether it is a vault is the Windows agent's to say; config must not guess.
	if cfg.IsVault() || cfg.IsExternal() || cfg.OwnsAgentKeys() {
		t.Errorf("a relayed agent is not a vault, external, or this config's to reconcile: %+v", cfg)
	}
	if cfg.VaultPathForAgent() != "" {
		t.Errorf("VaultPathForAgent: got %q, want none", cfg.VaultPathForAgent())
	}
}

// The relay listens on a socket of its own, so unlike "external" it has no
// SSH_AUTH_SOCK to fall back on.
func TestLoadConfig_windowsAgentTypeNeedsASocketPath(t *testing.T) {
	path := writeAgentConfig(t, "[agent]\ntype = \"windows\"\n")
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "socket_path") {
		t.Fatalf("want an error naming socket_path, got: %v", err)
	}
}

func TestLoadConfig_windowsAgentTypeNeedsNoKeysOrVault(t *testing.T) {
	path := writeAgentConfig(t, "[agent]\ntype = \"windows\"\nsocket_path = \"/tmp/relay.sock\"\n")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.WindowsSSHush != "" {
		t.Errorf("WindowsSSHush: got %q, want empty (sshush.exe is then found on PATH)", cfg.WindowsSSHush)
	}
}

func TestMarshalConfig_windowsAgentRoundTrip(t *testing.T) {
	in := Config{AgentType: AgentTypeWindows, SocketPath: "/tmp/relay.sock", WindowsSSHush: "/mnt/c/Tools/sshush.exe"}
	data, err := MarshalConfig(in)
	if err != nil {
		t.Fatalf("MarshalConfig: %v", err)
	}
	out, err := LoadConfig(writeAgentConfig(t, string(data)))
	if err != nil {
		t.Fatalf("LoadConfig: %v\n%s", err, data)
	}
	if out.AgentType != in.AgentType || out.WindowsSSHush != in.WindowsSSHush {
		t.Fatalf("round trip: got type %q windows_sshush %q", out.AgentType, out.WindowsSSHush)
	}

	// Other agent types do not grow a windows_sshush key.
	data, err = MarshalConfig(Config{AgentType: AgentTypeKeys, SocketPath: "/tmp/a.sock", KeyPaths: []string{"/tmp/k"}})
	if err != nil {
		t.Fatalf("MarshalConfig: %v", err)
	}
	if strings.Contains(string(data), "windows_sshush") {
		t.Fatalf("want no windows_sshush key for a keys agent, got:\n%s", data)
	}
}

func TestOwnsAgentKeys(t *testing.T) {
	for agentType, want := range map[string]bool{
		AgentTypeKeys:     true,
		AgentTypeVault:    true,
		AgentTypeExternal: false,
		AgentTypeWindows:  false,
	} {
		if got := (Config{AgentType: agentType}).OwnsAgentKeys(); got != want {
			t.Errorf("type %q: OwnsAgentKeys = %v, want %v", agentType, got, want)
		}
	}
}
