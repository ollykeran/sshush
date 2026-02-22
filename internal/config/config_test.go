package config

import (
	"os"
	"reflect"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestLoad(t *testing.T) {
	cases := []struct {
		name          string
		wantKeyPaths  []string
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

			cfg, err := Load(tmpPath)
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
}