package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/spf13/cobra"
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

func TestResolveNoColor_flag_activates_plain_mode(t *testing.T) {
	prev := style.IsPlainMode()
	defer style.SetPlainMode(prev)

	cmd := &cobra.Command{}
	cmd.Flags().Bool("no-color", false, "")
	cmd.SetArgs([]string{"--no-color"})
	_ = cmd.ParseFlags([]string{"--no-color"})

	resolveNoColor(cmd)
	if !style.IsPlainMode() {
		t.Fatal("expected plain mode after --no-color flag")
	}
}

func TestResolveNoColor_flag_false_does_not_activate(t *testing.T) {
	prev := style.IsPlainMode()
	defer style.SetPlainMode(prev)

	cmd := &cobra.Command{}
	cmd.Flags().Bool("no-color", false, "")
	cmd.SetArgs([]string{"--no-color=false"})
	_ = cmd.ParseFlags([]string{"--no-color=false"})

	resolveNoColor(cmd)
	if style.IsPlainMode() {
		t.Fatal("expected plain mode to remain false after --no-color=false")
	}
}

func TestResolveNoColor_env_activates_plain_mode(t *testing.T) {
	prev := style.IsPlainMode()
	defer style.SetPlainMode(prev)

	t.Setenv("NO_COLOR", "1")

	cmd := &cobra.Command{}
	cmd.Flags().Bool("no-color", false, "")
	resolveNoColor(cmd)
	if !style.IsPlainMode() {
		t.Fatal("expected plain mode after NO_COLOR=1")
	}
}

func TestResolveNoColor_env_empty_does_not_activate(t *testing.T) {
	prev := style.IsPlainMode()
	defer style.SetPlainMode(prev)

	t.Setenv("NO_COLOR", "")

	cmd := &cobra.Command{}
	cmd.Flags().Bool("no-color", false, "")
	resolveNoColor(cmd)
	if style.IsPlainMode() {
		t.Fatal("expected plain mode to remain false when NO_COLOR is empty")
	}
}

func TestResolveNoColor_env_unset_does_not_activate(t *testing.T) {
	prev := style.IsPlainMode()
	defer style.SetPlainMode(prev)

	cmd := &cobra.Command{}
	cmd.Flags().Bool("no-color", false, "")
	resolveNoColor(cmd)
	if style.IsPlainMode() {
		t.Fatal("expected plain mode to remain false when NO_COLOR is unset")
	}
}

func writeConfig(t *testing.T, path string, c config.Config) {
	t.Helper()
	data, err := config.MarshalConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
