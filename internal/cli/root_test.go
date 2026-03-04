package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/ollykeran/sshush/internal/config"
)

func TestLoadMergedConfig_noOverrides(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeConfig(t, path, config.Config{
		SocketPath: "/tmp/agent.sock",
		KeyPaths:   []string{"/tmp/key1"},
	})

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SocketPath != "/tmp/agent.sock" {
		t.Errorf("SocketPath: got %q", cfg.SocketPath)
	}
	if !reflect.DeepEqual(cfg.KeyPaths, []string{"/tmp/key1"}) {
		t.Errorf("KeyPaths: got %v", cfg.KeyPaths)
	}
}

func TestLoadMergedConfig_socketOverride(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeConfig(t, path, config.Config{
		SocketPath: "/from/file.sock",
		KeyPaths:   []string{"/tmp/key1"},
	})

	cfg, err := LoadMergedConfig(path, LoadOverrides{
		SocketPath: "/from/flag.sock",
		SocketSet:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SocketPath != "/from/flag.sock" {
		t.Errorf("SocketPath: got %q, want /from/flag.sock", cfg.SocketPath)
	}
}

func TestLoadMergedConfig_keyAppend(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeConfig(t, path, config.Config{
		SocketPath: "/tmp/sock",
		KeyPaths:   []string{"/config/key1"},
	})

	cfg, err := LoadMergedConfig(path, LoadOverrides{
		KeyPaths:    []string{"/cli/key2"},
		KeyPathsSet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/config/key1", "/cli/key2"}
	if !reflect.DeepEqual(cfg.KeyPaths, want) {
		t.Errorf("KeyPaths: got %v, want %v", cfg.KeyPaths, want)
	}
}

func TestLoadMergedConfig_missingFileNoOverrides_returnsError(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nonexistent.toml")
	_, err := LoadMergedConfig(path, LoadOverrides{})
	if err == nil {
		t.Fatal("expected error when config missing and no overrides")
	}
}

func TestLoadMergedConfig_missingFileWithKeyOverride_usesEmptyConfig(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nonexistent.toml")
	cfg, err := LoadMergedConfig(path, LoadOverrides{
		KeyPaths:    []string{"/tmp/key1"},
		KeyPathsSet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SocketPath != "" {
		t.Errorf("SocketPath: got %q, want empty", cfg.SocketPath)
	}
	if !reflect.DeepEqual(cfg.KeyPaths, []string{"/tmp/key1"}) {
		t.Errorf("KeyPaths: got %v", cfg.KeyPaths)
	}
}

func writeConfig(t *testing.T, path string, c config.Config) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(c); err != nil {
		t.Fatal(err)
	}
}
