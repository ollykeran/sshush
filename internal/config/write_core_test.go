package config

import (
	"os"
	"slices"
	"strings"
	"testing"
)

const coreSample = `# my sshush config
[agent]
# where the agent listens
socket_path = '\\.\pipe\sshush'
type = "keys"
key_paths = [
  "~/.ssh/id_ed25519", # work
  "~/.ssh/id_rsa",
]
shell = "powershell"

[server]
listen_port = 2222 # keep this

[theme]
name = "dracula"
`

func TestWriteCoreSettings_editsOnlyWhatChanged(t *testing.T) {
	path := writeConfig(t, coreSample)
	s, err := ReadCoreSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.AgentType != "keys" || len(s.KeyPaths) != 2 || s.SocketPath != `\\.\pipe\sshush` {
		t.Fatalf("read %+v", s)
	}
	s.KeyPaths = []string{`C:\keys\it's.pem`, "~/.ssh/id_ed25519"}
	s.SearchDirs = []string{`D:\keys`}
	if err := WriteCoreSettingsToPath(path, s); err != nil {
		t.Fatal(err)
	}
	text, cfg := readConfig(t, path)
	for _, keep := range []string{"# my sshush config", "# where the agent listens", "listen_port = 2222 # keep this", `name = "dracula"`, `shell = "powershell"`} {
		if !strings.Contains(text, keep) {
			t.Errorf("lost %q:\n%s", keep, text)
		}
	}
	if strings.Contains(text, "# work") {
		t.Errorf("the old multi-line key_paths was not replaced whole:\n%s", text)
	}
	got, _ := ReadCoreSettings(path)
	if !got.Equal(s) {
		t.Errorf("read back %+v, want %+v", got, s)
	}
	if cfg.ServerListenPort != 2222 || cfg.Theme.Name != "dracula" {
		t.Errorf("other settings changed: %+v", cfg)
	}
}

func TestWriteCoreSettings_vaultPathAddedAndRemoved(t *testing.T) {
	path := writeConfig(t, coreSample)
	s, _ := ReadCoreSettings(path)
	s.VaultPath = `C:\Users\me\vault.json`
	s.AgentType = AgentTypeVault
	if err := WriteCoreSettingsToPath(path, s); err != nil {
		t.Fatal(err)
	}
	_, cfg := readConfig(t, path)
	if !cfg.IsVault() || !strings.HasSuffix(cfg.VaultPath, "vault.json") {
		t.Fatalf("got %+v", cfg)
	}
	s.AgentType, s.VaultPath = AgentTypeKeys, ""
	if err := WriteCoreSettingsToPath(path, s); err != nil {
		t.Fatal(err)
	}
	text, cfg := readConfig(t, path)
	if cfg.VaultPath != "" || strings.Contains(text, "vault_path") {
		t.Errorf("vault_path not removed:\n%s", text)
	}
}

func TestWriteCoreSettings_leavesAnInvalidResultUnwritten(t *testing.T) {
	path := writeConfig(t, coreSample)
	s, _ := ReadCoreSettings(path)
	s.AgentType = AgentTypeVault // with no vault path
	if err := WriteCoreSettingsToPath(path, s); err == nil {
		t.Fatal("wrote a vault agent with no vault")
	}
	if data, _ := os.ReadFile(path); string(data) != coreSample {
		t.Errorf("the file changed:\n%s", data)
	}
}

func TestWriteCoreSettings_keepsWindowsLineEndsAndUTF16(t *testing.T) {
	crlf := strings.ReplaceAll(coreSample, "\n", "\r\n")
	path := writeConfig(t, string(encodeFile(crlf, encodingUTF16LE)))
	s, err := ReadCoreSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	s.SocketPath = `\\.\pipe\sshush2`
	if err := WriteCoreSettingsToPath(path, s); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	text, enc, ok := decodeTextFile(data)
	if !ok || enc != encodingUTF16LE {
		t.Fatalf("encoding changed: %v %v", enc, ok)
	}
	if strings.Count(text, "\r\n") != strings.Count(text, "\n") {
		t.Error("line ends changed")
	}
	if got, _ := ReadCoreSettings(path); got.SocketPath != `\\.\pipe\sshush2` {
		t.Errorf("socket path %q", got.SocketPath)
	}
}

func TestWriteCoreSettings_noChangeLeavesTextAlone(t *testing.T) {
	path := writeConfig(t, coreSample)
	s, _ := ReadCoreSettings(path)
	if err := WriteCoreSettingsToPath(path, s); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != coreSample {
		t.Errorf("the file changed:\n%s", data)
	}
}

func TestSetKey(t *testing.T) {
	text := "[agent]\ntype = \"keys\"\n  key_paths = [\"a\",\n  \"b # not a comment\"]\n[vault]\n"
	got := setKey(text, "agent", "key_paths", `key_paths = ["c"]`)
	want := "[agent]\ntype = \"keys\"\n  key_paths = [\"c\"]\n[vault]\n"
	if got != want {
		t.Errorf("got %q", got)
	}
	got = setKey(want, "agent", "search_dirs", `search_dirs = ["d"]`)
	if !slices.Contains(strings.Split(got, "\n"), `search_dirs = ["d"]`) || !strings.HasPrefix(got, "[agent]\nsearch_dirs") {
		t.Errorf("not added first in its table: %q", got)
	}
	if got := setKey(want, "server", "listen_port", "listen_port = 1"); !strings.HasSuffix(got, "[server]\nlisten_port = 1\n") {
		t.Errorf("table not added: %q", got)
	}
}
