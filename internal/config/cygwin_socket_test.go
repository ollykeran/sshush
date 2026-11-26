package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// The tray's switch: off writes cygwin_socket = "", on takes it out again,
// and a path of one's own is left alone while it stays on.
func TestWriteCoreSettings_cygwinSocket(t *testing.T) {
	path := writeConfig(t, coreSample)
	s, err := ReadCoreSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s.CygwinSocket || s.CygwinSocketPath != "" {
		t.Fatalf("a file without cygwin_socket: %+v", s)
	}
	s.CygwinSocket = false
	if err := WriteCoreSettingsToPath(path, s); err != nil {
		t.Fatal(err)
	}
	text, cfg := readConfig(t, path)
	if !strings.Contains(text, `cygwin_socket = ""`) || cfg.CygwinSocket != "" {
		t.Fatalf("off:\n%s", text)
	}
	s.CygwinSocket = true
	if err := WriteCoreSettingsToPath(path, s); err != nil {
		t.Fatal(err)
	}
	if text, _ := readConfig(t, path); strings.Contains(text, "cygwin_socket") {
		t.Fatalf("on again should leave the default:\n%s", text)
	}

	own := writeConfig(t, strings.Replace(coreSample, `type = "keys"`, "type = \"keys\"\ncygwin_socket = '~/my.sock'", 1))
	s, err = ReadCoreSettings(own)
	if err != nil {
		t.Fatal(err)
	}
	if !s.CygwinSocket || s.CygwinSocketPath != "~/my.sock" {
		t.Fatalf("own path: %+v", s)
	}
	s.PageantEmulation = false // another change, so something is written
	if err := WriteCoreSettingsToPath(own, s); err != nil {
		t.Fatal(err)
	}
	if text, _ := readConfig(t, own); !strings.Contains(text, "cygwin_socket = '~/my.sock'") {
		t.Fatalf("own path lost:\n%s", text)
	}
}
