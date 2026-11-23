package config

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/theme"
)

// writeSearchDirsConfig writes a minimal keys config with extra appended to [agent].
func writeSearchDirsConfig(t *testing.T, extra string) string {
	t.Helper()
	return writeAgentConfig(t, "[agent]\nsocket_path = \"/tmp/a.sock\"\ntype = \"keys\"\nkey_paths = []\n"+extra)
}

func TestLoadConfig_searchDirs(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory available")
	}
	abs := filepath.ToSlash(filepath.Join(t.TempDir(), "keys"))
	cases := []struct {
		name string
		line string
		want []string
	}{
		{"unset", "", nil},
		{"empty", "search_dirs = []\n", nil},
		{"tilde expanded", "search_dirs = [\"~/keys\", \"~\"]\n", []string{filepath.Join(home, "keys"), home}},
		{"absolute kept", "search_dirs = [" + strconv.Quote(abs) + "]\n", []string{abs}},
		{"blank dropped", "search_dirs = [\"\", \"  \", \"~/keys\"]\n", []string{filepath.Join(home, "keys")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadConfig(writeSearchDirsConfig(t, tc.line))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cfg.SearchDirs, tc.want) {
				t.Errorf("SearchDirs = %#v, want %#v", cfg.SearchDirs, tc.want)
			}
		})
	}

	t.Run("missing directory is not an error", func(t *testing.T) {
		if _, err := LoadConfig(writeSearchDirsConfig(t, "search_dirs = [\"~/definitely/not/here\"]\n")); err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
	})

	t.Run("a string instead of a list is an error", func(t *testing.T) {
		if _, err := LoadConfig(writeSearchDirsConfig(t, "search_dirs = \"~/keys\"\n")); err == nil {
			t.Fatal("expected an error for search_dirs given as a string")
		}
	})
}

func TestMarshalConfig_searchDirs(t *testing.T) {
	cfg := Config{SocketPath: "/tmp/a.sock", AgentType: AgentTypeKeys, KeyPaths: []string{}}
	data, err := MarshalConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "search_dirs") {
		t.Errorf("search_dirs written while unset:\n%s", data)
	}

	cfg.SearchDirs = []string{"/srv/keys", "/opt/keys"}
	data, err = MarshalConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := loadRenderedConfig(t, data)
	if !reflect.DeepEqual(got.SearchDirs, cfg.SearchDirs) {
		t.Errorf("SearchDirs round trip = %v, want %v", got.SearchDirs, cfg.SearchDirs)
	}
}

// TestRenderDefaultConfigBytes_searchDirsDocumented checks the default config
// documents search_dirs commented out, and that the example loads once
// uncommented.
func TestRenderDefaultConfigBytes_searchDirsDocumented(t *testing.T) {
	t.Parallel()
	data, err := renderDefaultConfigBytes("/run/user/1000/sshush.sock", nil, "posix", theme.DefaultTheme())
	if err != nil {
		t.Fatal(err)
	}
	line := regexp.MustCompile(`(?m)^# (search_dirs = .*)$`)
	if !line.Match(data) {
		t.Fatalf("default config has no commented-out search_dirs line:\n%s", data)
	}
	if cfg := loadRenderedConfig(t, data); cfg.SearchDirs != nil {
		t.Errorf("default config sets search_dirs: %v", cfg.SearchDirs)
	}
	cfg := loadRenderedConfig(t, line.ReplaceAll(data, []byte("$1")))
	if len(cfg.SearchDirs) == 0 {
		t.Error("uncommented search_dirs example did not load")
	}
}
