package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/platform"
)

// The default config holds a pipe name and Windows key paths, both full of
// backslashes: it has to survive being written as TOML and read back.
func TestWriteDefaultConfigFile_loadableOnWindows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(sshDir, "id_ed25519")
	if err := writeTestSSHKeyFile(key); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(home, "out", "config.toml")
	if err := WriteDefaultConfigFile(out, false); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(out)
	if err != nil {
		data, _ := os.ReadFile(out)
		t.Fatalf("LoadConfig: %v\n%s", err, data)
	}
	if cfg.SocketPath != platform.WindowsPipeName {
		t.Errorf("SocketPath: got %q, want %q", cfg.SocketPath, platform.WindowsPipeName)
	}
	if len(cfg.KeyPaths) != 1 || filepath.Clean(cfg.KeyPaths[0]) != key {
		t.Errorf("KeyPaths: got %v, want [%s]", cfg.KeyPaths, key)
	}
	if !platform.ValidShell(cfg.Shell) {
		t.Errorf("Shell: got %q", cfg.Shell)
	}
}

// SetupConfig on a machine with no PowerShell profile creates the config but no profile.
func TestSetupConfig_doesNotCreateAPowerShellProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	SetupConfig()

	if _, err := os.Stat(platform.DefaultConfigPath()); err != nil {
		t.Fatalf("expected the default config to be created: %v", err)
	}
	if !strings.HasPrefix(platform.DefaultConfigPath(), home) {
		t.Fatalf("config written outside the test home: %s", platform.DefaultConfigPath())
	}
	setup, ok := platform.ShellSetupForAutoSetup()
	if !ok {
		t.Fatal("expected a PowerShell profile path")
	}
	if _, err := os.Stat(setup.RcPath); !os.IsNotExist(err) {
		t.Fatalf("profile %s must not be created, stat err = %v", setup.RcPath, err)
	}
}

// With a profile already there, SetupConfig adds the start line to it.
func TestSetupConfig_appendsToAnExistingPowerShellProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	setup, ok := platform.ShellSetupForAutoSetup()
	if !ok {
		t.Fatal("expected a PowerShell profile path")
	}
	if err := os.MkdirAll(filepath.Dir(setup.RcPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(setup.RcPath, []byte("Set-Alias ll ls\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	SetupConfig()
	SetupConfig()

	data, err := os.ReadFile(setup.RcPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), platform.PowerShellSnippet) != 1 {
		t.Fatalf("expected the start line exactly once, got:\n%s", data)
	}
}

// On the run that creates the config, SetupConfig points ssh at the agent's
// pipe when there is a .ssh directory to do it in, and only then.
func TestSetupConfig_addsIdentityAgentToSSHConfig(t *testing.T) {
	for _, haveSSHDir := range []bool{true, false} {
		home := t.TempDir()
		t.Setenv("USERPROFILE", home)
		t.Setenv("XDG_CONFIG_HOME", "")
		sshDir := filepath.Join(home, ".ssh")
		if haveSSHDir {
			if err := os.MkdirAll(sshDir, 0o700); err != nil {
				t.Fatal(err)
			}
		}

		SetupConfig()

		data, err := os.ReadFile(filepath.Join(sshDir, "config"))
		if !haveSSHDir {
			if _, statErr := os.Stat(sshDir); !os.IsNotExist(statErr) {
				t.Fatalf(".ssh must not be created, stat err = %v", statErr)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "IdentityAgent //./pipe/sshush-agent\n") {
			t.Fatalf("unexpected ssh config:\n%s", data)
		}
	}
}
