package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestLoad(t *testing.T) {
	cases := []struct {
		name           string
		wantKeyPaths   []string
		wantSocketPath string
	}{
		{"default paths", []string{"/tmp/.ssh/id_rsa", "/tmp/.ssh/id_ed25519"}, "/tmp/.ssh/agent.sock"},
		{"single key", []string{"/home/user/.ssh/id_rsa"}, "/tmp/agent.sock"},
		{"custom paths", []string{"/a/key1", "/b/key2", "/c/key3"}, "/var/run/ssh-agent.sock"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := Config{
				KeyPaths:   tc.wantKeyPaths,
				SocketPath: tc.wantSocketPath,
			}
			tmp, err := os.CreateTemp("", "test-config-*.toml")
			if err != nil {
				t.Fatal(err)
			}
			tmpPath := tmp.Name()
			defer os.Remove(tmpPath)

			if err := toml.NewEncoder(tmp).Encode(content); err != nil {
				t.Fatal(err)
			}
			if err := tmp.Close(); err != nil {
				t.Fatal(err)
			}

			cfg, err := LoadConfig(tmpPath)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cfg.KeyPaths, tc.wantKeyPaths) {
				t.Errorf("KeyPaths: got %v, want %v", cfg.KeyPaths, tc.wantKeyPaths)
			}
			if cfg.SocketPath != tc.wantSocketPath {
				t.Errorf("SocketPath: got %q, want %q", cfg.SocketPath, tc.wantSocketPath)
			}
		})
	}

	t.Run("missing file returns error", func(t *testing.T) {
		tmp, err := os.CreateTemp("", "missing-config-*.toml")
		if err != nil {
			t.Fatal(err)
		}
		path := tmp.Name()
		tmp.Close()
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}

		if _, err := LoadConfig(path); err == nil {
			t.Fatal("expected error for missing config file")
		}
	})

	t.Run("tilde paths are expanded", func(t *testing.T) {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("no home directory available")
		}
		content := Config{
			KeyPaths:   []string{"~/foo/id_ed25519"},
			SocketPath: "~/.ssh/sshush.sock",
		}
		tmp, err := os.CreateTemp("", "tilde-config-*.toml")
		if err != nil {
			t.Fatal(err)
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)

		if err := toml.NewEncoder(tmp).Encode(content); err != nil {
			t.Fatal(err)
		}
		if err := tmp.Close(); err != nil {
			t.Fatal(err)
		}

		cfg, err := LoadConfig(tmpPath)
		if err != nil {
			t.Fatal(err)
		}
		if got := cfg.SocketPath; got != filepath.Join(home, ".ssh/sshush.sock") {
			t.Errorf("SocketPath: got %q, want %q", got, filepath.Join(home, ".ssh/sshush.sock"))
		}
		if len(cfg.KeyPaths) != 1 || cfg.KeyPaths[0] != filepath.Join(home, "foo/id_ed25519") {
			t.Errorf("KeyPaths: got %v, want [%q]", cfg.KeyPaths, filepath.Join(home, "foo/id_ed25519"))
		}
	})
}
