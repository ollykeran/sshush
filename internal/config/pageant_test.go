package config

import (
	"strings"
	"testing"
)

func TestLoadConfig_pageantEmulation(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		want       bool
	}{
		{"unset means on", "", true},
		{"true", "pageant_emulation = true\n", true},
		{"false", "pageant_emulation = false\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadConfig(writeSearchDirsConfig(t, tc.line))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.PageantEmulation != tc.want {
				t.Errorf("PageantEmulation = %v, want %v", cfg.PageantEmulation, tc.want)
			}
		})
	}
	t.Run("not a bool is an error", func(t *testing.T) {
		if _, err := LoadConfig(writeSearchDirsConfig(t, "pageant_emulation = \"yes\"\n")); err == nil {
			t.Fatal("expected an error")
		}
	})
}

// Off is written, so it survives a round trip; on is the default and is left
// out.
func TestMarshalConfig_pageantEmulation(t *testing.T) {
	base := Config{AgentType: AgentTypeKeys, SocketPath: "/tmp/a.sock", KeyPaths: []string{}}
	for _, on := range []bool{true, false} {
		cfg := base
		cfg.PageantEmulation = on
		data, err := MarshalConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(string(data), "pageant_emulation"); got == on {
			t.Errorf("on=%v: file mentions pageant_emulation = %v:\n%s", on, got, data)
		}
		back, err := LoadConfig(writeAgentConfig(t, string(data)))
		if err != nil {
			t.Fatal(err)
		}
		if back.PageantEmulation != on {
			t.Errorf("on=%v: read back %v", on, back.PageantEmulation)
		}
	}
}

func TestWriteCoreSettings_pageantEmulation(t *testing.T) {
	path := writeConfig(t, coreSample)
	s, err := ReadCoreSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s.PageantEmulation {
		t.Fatal("a file without pageant_emulation should read as on")
	}
	for _, on := range []bool{false, true} {
		s.PageantEmulation = on
		if err := WriteCoreSettingsToPath(path, s); err != nil {
			t.Fatal(err)
		}
		text, cfg := readConfig(t, path)
		if cfg.PageantEmulation != on {
			t.Errorf("wrote %v, loaded %v:\n%s", on, cfg.PageantEmulation, text)
		}
		for _, keep := range []string{"# my sshush config", "listen_port = 2222 # keep this"} {
			if !strings.Contains(text, keep) {
				t.Errorf("lost %q:\n%s", keep, text)
			}
		}
		if n := strings.Count(text, "pageant_emulation"); n != 1 {
			t.Errorf("pageant_emulation appears %d times:\n%s", n, text)
		}
	}
}
