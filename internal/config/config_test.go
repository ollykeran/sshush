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

	t.Run("marshal roundtrip preserves AgentVault and VaultPath", func(t *testing.T) {
		cfg := Config{
			SocketPath: "/tmp/s.sock",
			KeyPaths:   []string{"/tmp/k"},
			AgentVault: true,
			VaultPath:  "/tmp/v.json",
		}
		data, err := MarshalConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		tmp := filepath.Join(t.TempDir(), "cfg.toml")
		if err := os.WriteFile(tmp, data, 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := LoadConfig(tmp)
		if err != nil {
			t.Fatalf("LoadConfig: %v\n%s", err, string(data))
		}
		if !got.AgentVault || got.VaultPath != "/tmp/v.json" {
			t.Fatalf("got AgentVault=%v VaultPath=%q", got.AgentVault, got.VaultPath)
		}
	})

	t.Run("AgentBackendMode", func(t *testing.T) {
		cases := []struct {
			name string
			cfg  Config
			want string
		}{
			{"vault agent", Config{AgentVault: true, VaultPath: "/v.json"}, "vault"},
			{"keys only", Config{AgentVault: false, KeyPaths: []string{"/k"}, VaultPath: ""}, "keys"},
			{"offline vault path keys agent", Config{AgentVault: false, KeyPaths: []string{"/k"}, VaultPath: "/v.json"}, "keys"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if got := tc.cfg.AgentBackendMode(); got != tc.want {
					t.Fatalf("AgentBackendMode: got %q, want %q", got, tc.want)
				}
			})
		}
	})

	t.Run("vault_path with vault false keeps path for CLI", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.toml")
		body := "[agent]\nsocket_path = \"/tmp/agent.sock\"\nvault = false\nkey_paths = [\"/tmp/k\"]\n\n[vault]\nvault_path = \"/tmp/vault.json\"\n"
		if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.AgentVault {
			t.Fatal("expected AgentVault false")
		}
		if cfg.VaultPath != "/tmp/vault.json" {
			t.Fatalf("VaultPath: got %q", cfg.VaultPath)
		}
		if cfg.VaultPathForAgent() != "" {
			t.Fatalf("VaultPathForAgent: want empty, got %q", cfg.VaultPathForAgent())
		}
	})

	t.Run("relative socket_path is resolved against config file directory", func(t *testing.T) {
		dir := t.TempDir()
		cfgDir := filepath.Join(dir, "sshush")
		if err := os.MkdirAll(cfgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		cfgPath := filepath.Join(cfgDir, "config.toml")
		body := "[agent]\nsocket_path = \"sshush.sock\"\nkey_paths = [\"/tmp/dummy-key\"]\nvault = false\n"
		if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(cfgDir, "sshush.sock")
		if cfg.SocketPath != want {
			t.Fatalf("SocketPath: got %q, want %q", cfg.SocketPath, want)
		}
		if !filepath.IsAbs(cfg.SocketPath) {
			t.Fatalf("expected absolute socket path, got %q", cfg.SocketPath)
		}
	})
}
