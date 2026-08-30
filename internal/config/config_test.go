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
				AgentType:  AgentTypeKeys,
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
			AgentType:  AgentTypeKeys,
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
			AgentType:        AgentTypeKeys,
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

	t.Run("marshal roundtrip preserves AgentType vault and VaultPath", func(t *testing.T) {
		cfg := Config{
			SocketPath: "/tmp/s.sock",
			KeyPaths:   []string{"/tmp/k"},
			AgentType:  AgentTypeVault,
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
		if !got.IsVault() || got.VaultPath != "/tmp/v.json" {
			t.Fatalf("got AgentType=%q VaultPath=%q", got.AgentType, got.VaultPath)
		}
	})

	t.Run("AgentBackendMode", func(t *testing.T) {
		cases := []struct {
			name string
			cfg  Config
			want string
		}{
			{"vault agent", Config{AgentType: AgentTypeVault, VaultPath: "/v.json"}, "vault"},
			{"keys only", Config{AgentType: AgentTypeKeys, KeyPaths: []string{"/k"}, VaultPath: ""}, "keys"},
			{"offline vault path keys agent", Config{AgentType: AgentTypeKeys, KeyPaths: []string{"/k"}, VaultPath: "/v.json"}, "keys"},
			{"external agent", Config{AgentType: AgentTypeExternal}, "keys"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if got := tc.cfg.AgentBackendMode(); got != tc.want {
					t.Fatalf("AgentBackendMode: got %q, want %q", got, tc.want)
				}
			})
		}
	})

	t.Run("vault_path with type keys keeps path for CLI", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.toml")
		body := "[agent]\nsocket_path = \"/tmp/agent.sock\"\ntype = \"keys\"\nkey_paths = [\"/tmp/k\"]\n\n[vault]\nvault_path = \"/tmp/vault.json\"\n"
		if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.IsVault() {
			t.Fatal("expected AgentType to not be vault")
		}
		if cfg.VaultPath != "/tmp/vault.json" {
			t.Fatalf("VaultPath: got %q", cfg.VaultPath)
		}
		if cfg.VaultPathForAgent() != "" {
			t.Fatalf("VaultPathForAgent: want empty, got %q", cfg.VaultPathForAgent())
		}
	})

	t.Run("theme.no_color is parsed from config file", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.toml")
		body := "[agent]\nsocket_path = \"/tmp/agent.sock\"\ntype = \"keys\"\nkey_paths = [\"/tmp/k\"]\n\n[theme]\nno_color = true\n"
		if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Theme.NoColor {
			t.Fatal("expected Theme.NoColor to be true")
		}
	})

	t.Run("theme.no_color defaults to false when absent", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.toml")
		body := "[agent]\nsocket_path = \"/tmp/agent.sock\"\ntype = \"keys\"\nkey_paths = [\"/tmp/k\"]\n"
		if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Theme.NoColor {
			t.Fatal("expected Theme.NoColor to default to false")
		}
	})

	t.Run("theme.no_color roundtrips through marshal", func(t *testing.T) {
		cfg := Config{
			SocketPath: "/tmp/agent.sock",
			KeyPaths:   []string{"/tmp/k"},
			AgentType:  AgentTypeKeys,
			Theme:      ThemeSection{NoColor: true},
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
			t.Fatal(err)
		}
		if !got.Theme.NoColor {
			t.Fatal("expected Theme.NoColor to be true after roundtrip")
		}
	})

	t.Run("marshal roundtrip preserves AgentType external", func(t *testing.T) {
		cfg := Config{
			SocketPath: "/tmp/s.sock",
			KeyPaths:   []string{"/tmp/k"},
			AgentType:  AgentTypeExternal,
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
		if !got.IsExternal() {
			t.Fatalf("got AgentType=%q, want %q", got.AgentType, AgentTypeExternal)
		}
	})

	t.Run("external type allows empty socket_path", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.toml")
		body := "[agent]\ntype = \"external\"\n"
		if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(cfgPath)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.SocketPath != "" {
			t.Fatalf("SocketPath: got %q, want empty (resolved from SSH_AUTH_SOCK at the CLI layer)", cfg.SocketPath)
		}
		if !cfg.IsExternal() {
			t.Fatalf("got AgentType=%q, want %q", cfg.AgentType, AgentTypeExternal)
		}
	})

	t.Run("keys type still requires socket_path", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.toml")
		body := "[agent]\ntype = \"keys\"\nkey_paths = [\"/tmp/k\"]\n"
		if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(cfgPath); err == nil {
			t.Fatal("expected error when socket_path is missing for type = \"keys\"")
		}
	})

	t.Run("missing type is rejected", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.toml")
		// Old-style config using the removed vault/external booleans, no type set.
		body := "[agent]\nsocket_path = \"/tmp/agent.sock\"\nvault = true\nexternal = true\n\n[vault]\nvault_path = \"/tmp/vault.json\"\n"
		if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(cfgPath); err == nil {
			t.Fatal("expected error when [agent].type is missing")
		}
	})

	t.Run("invalid type value is rejected", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.toml")
		body := "[agent]\nsocket_path = \"/tmp/agent.sock\"\ntype = \"bogus\"\nkey_paths = [\"/tmp/k\"]\n"
		if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(cfgPath); err == nil {
			t.Fatal("expected error for invalid [agent].type value")
		}
	})

	t.Run("relative socket_path is resolved against config file directory", func(t *testing.T) {
		dir := t.TempDir()
		cfgDir := filepath.Join(dir, "sshush")
		if err := os.MkdirAll(cfgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		cfgPath := filepath.Join(cfgDir, "config.toml")
		body := "[agent]\nsocket_path = \"sshush.sock\"\nkey_paths = [\"/tmp/dummy-key\"]\ntype = \"keys\"\n"
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
