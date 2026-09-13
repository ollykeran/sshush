package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/config"
)

func TestServerEnableSteps(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []enableStep
	}{
		{
			name:    "no server table at all",
			content: "[agent]\ntype = \"keys\"\n",
			want:    []enableStep{{0, "add a [server] table with listen_port = 2222"}},
		},
		{
			name:    "commented-out block, as generated",
			content: "[agent]\n\n# [server]\n# TCP port to listen on.\n# listen_port = 2222\n# shell = \"/bin/zsh\"\n\n[theme]\n",
			want:    []enableStep{{3, "uncomment [server]"}, {5, "uncomment listen_port = 2222"}},
		},
		{
			name:    "live table with listen_port commented out",
			content: "[server]\n#listen_port = 2200\n",
			want:    []enableStep{{2, "uncomment listen_port = 2200"}},
		},
		{
			name:    "live table without listen_port",
			content: "[server]\nhost_key = \"~/k\"\n",
			want:    []enableStep{{1, "add listen_port = 2222 under [server]"}},
		},
		{
			name:    "listen_port set to 0",
			content: "[server]\nlisten_port = 0\n",
			want:    []enableStep{{2, "set a port above 0, e.g. listen_port = 2222"}},
		},
		{
			name:    "live table preferred over a commented-out one",
			content: "# [server]\n# listen_port = 2222\n\n[theme]\n\n[server]\nshell = \"sh\"\n",
			want:    []enableStep{{6, "add listen_port = 2222 under [server]"}},
		},
		{
			name:    "listen_port under another table does not count",
			content: "[agent]\n# listen_port = 2222\n\n# [server]\n",
			want:    []enableStep{{4, "uncomment [server]"}, {4, "add listen_port = 2222 under [server]"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := serverEnableSteps(tc.content); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("serverEnableSteps() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestServerEnableSteps_OnTheGeneratedConfig checks the hint against the file sshush
// actually writes: the lines it names are the right ones, and making exactly those
// edits enables the server.
func TestServerEnableSteps_OnTheGeneratedConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.WriteDefaultConfigFile(path, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	steps := serverEnableSteps(string(data))
	if len(steps) != 2 {
		t.Fatalf("steps = %+v, want uncommenting [server] and listen_port", steps)
	}
	lines := strings.Split(string(data), "\n")
	for _, step := range steps {
		if !strings.HasPrefix(step.edit, "uncomment ") {
			t.Fatalf("step %+v is not an uncomment", step)
		}
		lines[step.line-1] = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[step.line-1]), "#"))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig after the suggested edits: %v", err)
	}
	if cfg.ServerListenPort != 2222 {
		t.Errorf("ServerListenPort after the suggested edits = %d, want 2222", cfg.ServerListenPort)
	}
}

func TestServerEnableHint_NamesTheFileAndLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[agent]\n\n[server]\nlisten_port = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := serverEnableHint(path)
	if len(got) != 1 || !strings.HasSuffix(got[0], "config.toml:4: set a port above 0, e.g. listen_port = 2222") {
		t.Errorf("serverEnableHint() = %q, want one line naming config.toml:4", got)
	}
}
