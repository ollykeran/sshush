package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/ollykeran/sshush/internal/utils"
)

func TestCreateDefaultConfig_socketPathAbsoluteWithoutXDG(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", "")

	if err := CreateDefaultConfig(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(tmp, ".config")) })

	data, err := os.ReadFile(filepath.Join(tmp, ".config", "sshush", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		SocketPath string `toml:"socket_path"`
	}
	if err := toml.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.SocketPath == "" {
		t.Fatal("empty socket_path")
	}
	if !strings.HasPrefix(parsed.SocketPath, "~") {
		t.Fatalf("expected contracted socket_path under home, got %q", parsed.SocketPath)
	}
	expanded := utils.ExpandHomeDirectory(parsed.SocketPath)
	if !filepath.IsAbs(expanded) {
		t.Fatalf("expanded socket_path not absolute: %q", expanded)
	}
}
