package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
			cfg := Config{
				KeyPaths:   tc.wantKeyPaths,
				SocketPath: tc.wantSocketPath,
			}
			data, err := MarshalConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}
			tmp, err := os.CreateTemp("", "test-config-*.toml")
			if err != nil {
				t.Fatal(err)
			}
			tmpPath := tmp.Name()
			defer os.Remove(tmpPath)
			if _, err := tmp.Write(data); err != nil {
				t.Fatal(err)
			}
			if err := tmp.Close(); err != nil {
				t.Fatal(err)
			}

			got, err := LoadConfig(tmpPath)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.KeyPaths, tc.wantKeyPaths) {
				t.Errorf("KeyPaths: got %v, want %v", got.KeyPaths, tc.wantKeyPaths)
			}
			if got.SocketPath != tc.wantSocketPath {
				t.Errorf("SocketPath: got %q, want %q", got.SocketPath, tc.wantSocketPath)
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
		cfg := Config{
			KeyPaths:   []string{"~/foo/id_ed25519"},
			SocketPath: "~/.ssh/sshush.sock",
		}
		data, err := MarshalConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		tmp, err := os.CreateTemp("", "tilde-config-*.toml")
		if err != nil {
			t.Fatal(err)
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)
		if _, err := tmp.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := tmp.Close(); err != nil {
			t.Fatal(err)
		}

		loaded, err := LoadConfig(tmpPath)
		if err != nil {
			t.Fatal(err)
		}
		if got := loaded.SocketPath; got != filepath.Join(home, ".ssh/sshush.sock") {
			t.Errorf("SocketPath: got %q, want %q", got, filepath.Join(home, ".ssh/sshush.sock"))
		}
		if len(loaded.KeyPaths) != 1 || loaded.KeyPaths[0] != filepath.Join(home, "foo/id_ed25519") {
			t.Errorf("KeyPaths: got %v, want [%q]", loaded.KeyPaths, filepath.Join(home, "foo/id_ed25519"))
		}
	})

	t.Run("listen_port under server section", func(t *testing.T) {
		cfg := Config{
			KeyPaths:         []string{"/tmp/id_ed25519"},
			SocketPath:       "/tmp/agent.sock",
			ServerListenPort: 2222,
		}
		data, err := MarshalConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		tmp, err := os.CreateTemp("", "server-config-*.toml")
		if err != nil {
			t.Fatal(err)
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)
		if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadConfig(tmpPath)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.ServerListenPort != 2222 {
			t.Errorf("ServerListenPort: got %d, want 2222", loaded.ServerListenPort)
		}
	})
}
