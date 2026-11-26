package tray

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/config"
)

func problemFor(problems []FieldProblem, field uint16) *FieldProblem {
	_, p := settingsCaption(field, problems)
	return p
}

func TestSettingsProblems(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(key, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	good := config.CoreSettings{AgentType: config.AgentTypeKeys, SocketPath: `\\.\pipe\sshush`, KeyPaths: []string{key}}
	if p := settingsProblems(good); len(p) != 0 {
		t.Fatalf("good settings: %+v", p)
	}

	tests := []struct {
		name    string
		edit    func(*config.CoreSettings)
		field   uint16
		blocker bool
	}{
		{"no type", func(s *config.CoreSettings) { s.AgentType = "" }, idSetTypeCaption, true},
		{"no pipe", func(s *config.CoreSettings) { s.SocketPath = "" }, idSetSocketCaption, true},
		{"relative pipe", func(s *config.CoreSettings) { s.SocketPath = "sshush" }, idSetSocketCaption, true},
		{"spaces", func(s *config.CoreSettings) { s.SocketPath += " " }, idSetSocketCaption, true},
		{"vault without a file", func(s *config.CoreSettings) { s.AgentType = config.AgentTypeVault }, idSetVaultCaption, true},
		{"relative vault", func(s *config.CoreSettings) { s.VaultPath = "vault.json" }, idSetVaultCaption, true},
		{"vault not created yet", func(s *config.CoreSettings) {
			s.AgentType, s.VaultPath = config.AgentTypeVault, filepath.Join(dir, "vault.json")
		}, idSetVaultCaption, false},
		{"missing key", func(s *config.CoreSettings) { s.KeyPaths = append(s.KeyPaths, filepath.Join(dir, "gone")) }, idSetKeysCaption, false},
		{"folder for a key", func(s *config.CoreSettings) { s.KeyPaths = []string{dir} }, idSetKeysCaption, false},
		{"missing folder", func(s *config.CoreSettings) { s.SearchDirs = []string{filepath.Join(dir, "nope")} }, idSetDirsCaption, false},
	}
	for _, tc := range tests {
		s := good
		s.KeyPaths = append([]string(nil), good.KeyPaths...)
		tc.edit(&s)
		problems := settingsProblems(s)
		p := problemFor(problems, tc.field)
		if p == nil {
			t.Errorf("%s: no problem on %d: %+v", tc.name, tc.field, problems)
			continue
		}
		if p.Blocker != tc.blocker || blocked(problems) != tc.blocker {
			t.Errorf("%s: blocker %v, want %v", tc.name, p.Blocker, tc.blocker)
		}
	}

	// An external agent needs no pipe of sshush's.
	ext := good
	ext.AgentType, ext.SocketPath = config.AgentTypeExternal, ""
	if blocked(settingsProblems(ext)) {
		t.Error("an external agent with no pipe is blocked")
	}
}

func TestSettingsCaptionDefaults(t *testing.T) {
	if text, p := settingsCaption(idSetKeysCaption, nil); p != nil || !strings.Contains(text, "key files") {
		t.Errorf("got %q %v", text, p)
	}
}

func TestNeedsRestart(t *testing.T) {
	a := config.CoreSettings{AgentType: "keys", SocketPath: "x", KeyPaths: []string{"k"}}
	b := a
	b.SearchDirs = []string{"d"}
	if needsRestart(a, b) {
		t.Error("search folders need no restart")
	}
	b.KeyPaths = []string{"k", "l"}
	if !needsRestart(a, b) {
		t.Error("new key files need a restart")
	}
}

func TestAddPathsSkipsDuplicates(t *testing.T) {
	list, n := addPaths([]string{"/a/b"}, "/a/b", "/a/c", "/a/c", "")
	if n != 1 || len(list) != 2 {
		t.Errorf("got %v, %d", list, n)
	}
}

func TestAgentTypeChoices(t *testing.T) {
	if c := agentTypeChoices(config.AgentTypeKeys); len(c) != 2 {
		t.Errorf("an agent of another program is offered to a file that does not say it: %+v", c)
	}
	if c := agentTypeChoices(config.AgentTypeExternal); c[len(c)-1].value != config.AgentTypeExternal {
		t.Error("a file that says external does not get it offered")
	}
	if c := agentTypeChoices(config.AgentTypeWindows); c[len(c)-1].value != config.AgentTypeWindows {
		t.Error("a file that says windows does not get it offered")
	}
}

func TestIsPipeName(t *testing.T) {
	for p, want := range map[string]bool{`\\.\pipe\sshush`: true, `//./pipe/x`: true, `\\.\pipe\`: false, `C:\x`: false} {
		if isPipeName(p) != want {
			t.Errorf("isPipeName(%q) = %v", p, !want)
		}
	}
}
