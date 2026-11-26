package tui

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/utils"
	"golang.org/x/crypto/ssh"
)

// writeRescanTestKey writes an unencrypted ed25519 private key to path.
func writeRescanTestKey(t *testing.T, path string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "rescan-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
}

// rescanTestConfig points HOME at an empty directory (so ~/.ssh holds nothing)
// and writes a config whose [agent].search_dirs lists keyDir and a directory
// that does not exist. It returns the config's path and the missing directory.
func rescanTestConfig(t *testing.T, keyDir string) (configPath, missing string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	missing = filepath.Join(home, "no-such-dir")
	configPath = filepath.Join(t.TempDir(), "config.toml")
	content := "[agent]\nsocket_path = " + strconv.Quote(filepath.ToSlash(filepath.Join(home, "agent.sock"))) +
		"\ntype = \"keys\"\nkey_paths = []\nsearch_dirs = [" +
		strconv.Quote(filepath.ToSlash(keyDir)) + ", " + strconv.Quote(filepath.ToSlash(missing)) + "]\n"
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return configPath, missing
}

func TestAgentRescanKeySearchesConfiguredDirs(t *testing.T) {
	keyDir := t.TempDir()
	keyPath := filepath.Join(keyDir, "id_elsewhere")
	writeRescanTestKey(t, keyPath)
	configPath, missing := rescanTestConfig(t, keyDir)

	sk, agent := newAgentTestSkeleton()
	agent.configPath = configPath

	// Through the Skeleton, so a global hotkey claiming the key would show.
	_, cmd := sk.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if cmd == nil {
		t.Fatal("f on the Agent tab returned no command")
	}
	if agent.status != "searching for keys..." {
		t.Fatalf("status = %q, want the searching note", agent.status)
	}

	msg, ok := discoverKeysCmd(configPath, true)().(foundKeysMsg)
	if !ok {
		t.Fatal("discoverKeysCmd did not return a foundKeysMsg")
	}
	if !msg.rescan {
		t.Error("a re-scan's foundKeysMsg should be marked rescan")
	}
	if len(msg.skipped) != 1 || filepath.Clean(msg.skipped[0]) != missing {
		t.Errorf("skipped = %v, want [%s]", msg.skipped, missing)
	}

	agent.Update(msg)
	found := false
	for _, kp := range agent.foundKeys {
		if kp.Path == keyPath {
			found = true
		}
	}
	if !found {
		t.Fatalf("foundKeys = %v, want it to include %s", agent.foundKeys, keyPath)
	}
	if !strings.HasPrefix(agent.status, "found 1 new key") || !strings.Contains(agent.status, "skipped unreadable") {
		t.Errorf("status = %q, want the found count and the skipped directory", agent.status)
	}
	if agent.statusErr {
		t.Error("a skipped directory should not be reported as an error")
	}
}

func TestAgentRescanKeyCommandRunsDiscovery(t *testing.T) {
	keyDir := t.TempDir()
	writeRescanTestKey(t, filepath.Join(keyDir, "id_a"))
	configPath, _ := rescanTestConfig(t, keyDir)

	_, agent := newAgentTestSkeleton()
	agent.configPath = configPath

	_, cmd := agent.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if cmd == nil {
		t.Fatal("f returned no command")
	}
	msg, ok := cmd().(foundKeysMsg)
	if !ok {
		t.Fatalf("f's command returned %T, want foundKeysMsg", cmd())
	}
	if len(msg.paths) != 1 || !msg.rescan {
		t.Fatalf("got %d paths (rescan=%v), want the one key in search_dirs", len(msg.paths), msg.rescan)
	}
}

func TestAgentStartupDiscoveryIsQuiet(t *testing.T) {
	keyDir := t.TempDir()
	writeRescanTestKey(t, filepath.Join(keyDir, "id_a"))
	configPath, _ := rescanTestConfig(t, keyDir)

	_, agent := newAgentTestSkeleton()
	agent.status = "2 key(s) loaded"
	msg := discoverKeysCmd(configPath, false)().(foundKeysMsg)
	if len(msg.paths) != 1 {
		t.Fatalf("startup discovery found %d keys, want the one in search_dirs", len(msg.paths))
	}
	agent.Update(msg)
	if agent.status != "2 key(s) loaded" {
		t.Errorf("startup discovery changed the status to %q", agent.status)
	}
}

func TestDiscoverKeysCmdWithoutConfig(t *testing.T) {
	msg := discoverKeysCmd(filepath.Join(t.TempDir(), "missing.toml"), true)().(foundKeysMsg)
	if len(msg.skipped) != 0 {
		t.Errorf("skipped = %v, want none without a config", msg.skipped)
	}
}

func TestAgentRescanControlClickable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	_, agent := newAgentTestSkeleton()
	agent.width = 80
	agent.height = 24

	zone.Scan(agent.View().Content)
	z := waitForZone(agent.rescanZoneID())
	if z == nil {
		t.Fatal("re-scan control zone not registered")
	}
	x, y := (z.StartX+z.EndX)/2, z.StartY

	if _, cmd := agent.handleMouse(x, y, false); cmd != nil {
		t.Error("the press of a click should not search; only its release")
	}
	_, cmd := agent.handleMouse(x, y, true)
	if cmd == nil {
		t.Fatal("releasing a click on the re-scan control returned no command")
	}
	if _, ok := cmd().(foundKeysMsg); !ok {
		t.Error("the re-scan control's command is not a key search")
	}
}

func TestAgentHelpListsRescanKey(t *testing.T) {
	_, agent := newAgentTestSkeleton()
	help := strings.Join(agent.HelpEntries(), "\n")
	if !strings.Contains(help, "Search again for keys") {
		t.Errorf("Agent help has no re-scan row:\n%s", help)
	}
}

func TestRescanStatus(t *testing.T) {
	nope := filepath.Join(t.TempDir(), "nope")
	cases := []struct {
		found   int
		skipped []string
		want    string
	}{
		{0, nil, "no new keys found"},
		{1, nil, "found 1 new key"},
		{3, nil, "found 3 new keys"},
		{2, []string{nope}, "found 2 new keys (skipped unreadable: " + utils.DisplayPath(nope) + ")"},
	}
	for _, tc := range cases {
		if got := rescanStatus(tc.found, tc.skipped); got != tc.want {
			t.Errorf("rescanStatus(%d, %v) = %q, want %q", tc.found, tc.skipped, got, tc.want)
		}
	}
}
