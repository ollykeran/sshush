package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/theme"
)

// writeConfig writes text as config.toml in a temp directory.
func writeConfig(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readConfig(t *testing.T, path string) (string, Config) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load edited config: %v\n%s", err, data)
	}
	return string(data), cfg
}

func TestWriteAgentType_defaultConfigKeepsItsComments(t *testing.T) {
	gen, err := renderDefaultConfigBytes("/tmp/sshush.sock", nil, "posix", theme.DefaultTheme())
	if err != nil {
		t.Fatal(err)
	}
	path := writeConfig(t, string(gen))
	vaultPath := `C:\Users\me\AppData\Roaming\sshush\vault.json`
	if err := WriteAgentTypeToPath(path, AgentTypeVault, vaultPath); err != nil {
		t.Fatal(err)
	}
	text, cfg := readConfig(t, path)
	if cfg.AgentType != AgentTypeVault || cfg.VaultPath != vaultPath {
		t.Errorf("type %q vault %q", cfg.AgentType, cfg.VaultPath)
	}
	// Every comment line of the original is still there.
	for _, line := range strings.Split(string(gen), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") && strings.TrimSpace(line) != "# [vault]" && !strings.Contains(text, line) {
			t.Errorf("lost comment %q", line)
		}
	}
	// The vault table took the example's place rather than going at the end.
	if strings.Index(text, "[vault]") > strings.Index(text, "[theme]") {
		t.Errorf("[vault] after [theme]:\n%s", text)
	}

	// And back: the vault path stays, for the vault commands.
	if err := WriteAgentTypeToPath(path, AgentTypeKeys, ""); err != nil {
		t.Fatal(err)
	}
	_, cfg = readConfig(t, path)
	if cfg.AgentType != AgentTypeKeys || cfg.VaultPath != vaultPath {
		t.Errorf("back to keys: type %q vault %q", cfg.AgentType, cfg.VaultPath)
	}
}

func TestWriteAgentType_keepsTheVaultTheUserChose(t *testing.T) {
	path := writeConfig(t, "[agent]\nsocket_path = \"/s\"\ntype = \"keys\" # set by hand\nkey_paths = []\n\n[vault]\nvault_path = \"~/mine.json\"\n")
	if err := WriteAgentTypeToPath(path, AgentTypeVault, "/other/vault.json"); err != nil {
		t.Fatal(err)
	}
	text, cfg := readConfig(t, path)
	if !strings.HasSuffix(cfg.VaultPath, "mine.json") {
		t.Errorf("vault path %q, want the configured one kept", cfg.VaultPath)
	}
	if strings.Count(text, "type =") != 1 {
		t.Errorf("type set twice:\n%s", text)
	}
}

func TestWriteAgentType_addsTablesThatAreMissing(t *testing.T) {
	path := writeConfig(t, "[agent]\nsocket_path = \"/s\"\nkey_paths = []\ntype = \"keys\"\n")
	if err := WriteAgentTypeToPath(path, AgentTypeVault, "/v/vault.json"); err != nil {
		t.Fatal(err)
	}
	if _, cfg := readConfig(t, path); cfg.VaultPath != "/v/vault.json" || !cfg.IsVault() {
		t.Errorf("got %+v", cfg)
	}
}

func TestWriteAgentType_keepsWindowsLineEnds(t *testing.T) {
	path := writeConfig(t, "[agent]\r\nsocket_path = '\\\\.\\pipe\\x'\r\ntype = \"keys\"\r\nkey_paths = []\r\n")
	if err := WriteAgentTypeToPath(path, AgentTypeVault, `C:\v\vault.json`); err != nil {
		t.Fatal(err)
	}
	text, cfg := readConfig(t, path)
	if strings.Count(text, "\n") != strings.Count(text, "\r\n") {
		t.Errorf("mixed line ends: %q", text)
	}
	if cfg.VaultPath != `C:\v\vault.json` {
		t.Errorf("vault path %q", cfg.VaultPath)
	}
}

func TestWriteAgentType_leavesAnInvalidResultUnwritten(t *testing.T) {
	// Vault mode needs a vault path, and none is given or configured.
	orig := "[agent]\nsocket_path = \"/s\"\ntype = \"keys\"\nkey_paths = []\n"
	path := writeConfig(t, orig)
	if err := WriteAgentTypeToPath(path, AgentTypeVault, ""); err == nil {
		t.Fatal("no error")
	}
	if data, _ := os.ReadFile(path); string(data) != orig {
		t.Errorf("file changed:\n%s", data)
	}
}

func TestWriteAgentType_missingFile(t *testing.T) {
	if err := WriteAgentTypeToPath(filepath.Join(t.TempDir(), "nope.toml"), AgentTypeKeys, ""); err == nil {
		t.Error("no error")
	}
}
