package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ollykeran/sshush/internal/platform"
)

func TestLoadConfig_cygwinSocket(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory available")
	}
	onWindows := func(want string) string {
		if runtime.GOOS != "windows" {
			return "" // there is no such thing elsewhere
		}
		return want
	}
	for _, tc := range []struct {
		name, line, want string
	}{
		{"unset means the default", "", onWindows(platform.DefaultCygwinSocketPath())},
		{"empty turns it off", "cygwin_socket = \"\"\n", ""},
		{"tilde expanded", "cygwin_socket = \"~/agent.sock\"\n", onWindows(filepath.Join(home, "agent.sock"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadConfig(writeSearchDirsConfig(t, tc.line))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.CygwinSocket != tc.want {
				t.Errorf("CygwinSocket = %q, want %q", cfg.CygwinSocket, tc.want)
			}
		})
	}
}

// Off and a path of one's own survive a round trip; the default is left out.
func TestMarshalConfig_cygwinSocket(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cygwin_socket is Windows only")
	}
	base := Config{AgentType: AgentTypeKeys, SocketPath: `\\.\pipe\x`, KeyPaths: []string{}, PageantEmulation: true}
	for _, path := range []string{platform.DefaultCygwinSocketPath(), "", filepath.Join(t.TempDir(), "mine.sock")} {
		cfg := base
		cfg.CygwinSocket = path
		data, err := MarshalConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		back, err := LoadConfig(writeAgentConfig(t, string(data)))
		if err != nil {
			t.Fatal(err)
		}
		if back.CygwinSocket != path {
			t.Errorf("wrote %q, read back %q:\n%s", path, back.CygwinSocket, data)
		}
	}
}
