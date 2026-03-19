package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/theme"
	ssh "golang.org/x/crypto/ssh"
)

func TestKeyPathsToTOMLArray(t *testing.T) {
	t.Parallel()
	if got := keyPathsToTOMLArray(nil); got != "[]" {
		t.Errorf("nil: got %q, want []", got)
	}
	if got := keyPathsToTOMLArray([]string{}); got != "[]" {
		t.Errorf("empty: got %q, want []", got)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{filepath.Join(home, ".ssh", "id_ed25519"), "/other/key"}
	got := keyPathsToTOMLArray(keys)
	want := `["~/.ssh/id_ed25519", "/other/key"]`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderDefaultConfigBytes_loads(t *testing.T) {
	t.Parallel()
	data, err := renderDefaultConfigBytes("/run/user/1000/sshush.sock", []string{"/tmp/id_ed25519"}, theme.DefaultTheme())
	if err != nil {
		t.Fatal(err)
	}
	tmp, err := os.CreateTemp("", "default-render-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	path := tmp.Name()
	defer os.Remove(path)
	if _, err := tmp.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v\n%s", err, string(data))
	}
	if cfg.SocketPath != "/run/user/1000/sshush.sock" {
		t.Errorf("SocketPath: got %q", cfg.SocketPath)
	}
	if len(cfg.KeyPaths) != 1 || cfg.KeyPaths[0] != "/tmp/id_ed25519" {
		t.Errorf("KeyPaths: got %v", cfg.KeyPaths)
	}
	if cfg.Theme.Name != "default" {
		t.Errorf("Theme.Name: got %q", cfg.Theme.Name)
	}
}

func TestStandardConfigFile_suffix(t *testing.T) {
	got := StandardConfigFile()
	if !strings.HasSuffix(got, filepath.Join(".config", "sshush", "config.toml")) {
		t.Errorf("StandardConfigFile: got %q", got)
	}
}

func TestWriteDefaultConfigFile_existsNoOverwrite(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := WriteDefaultConfigFile(path, false)
	if err == nil {
		t.Fatal("expected error when file exists")
	}
	var se *style.StyledError
	if !errors.As(err, &se) {
		t.Errorf("expected StyledError, got %T", err)
	}
}

func TestWriteDefaultConfigFile_overwrite(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	if err := WriteDefaultConfigFile(path, false); err != nil {
		t.Fatal(err)
	}
	if err := WriteDefaultConfigFile(path, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[agent]") {
		t.Errorf("expected [agent] in written file")
	}
}

func TestWriteDefaultConfigFile_loadableWithHomeSSHKey(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeTestSSHKeyFile(filepath.Join(sshDir, "id_ed25519")); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(tmp, "out", "config.toml")
	if err := WriteDefaultConfigFile(out, false); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(out)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !strings.HasSuffix(cfg.SocketPath, "sshush.sock") {
		t.Errorf("SocketPath: got %q, want suffix sshush.sock", cfg.SocketPath)
	}
	if len(cfg.KeyPaths) < 1 {
		t.Errorf("expected at least one key path, got %v", cfg.KeyPaths)
	}
}

func writeTestSSHKeyFile(privPath string) error {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	block, err := ssh.MarshalPrivateKey(priv, "test")
	if err != nil {
		return err
	}
	return os.WriteFile(privPath, pem.EncodeToMemory(block), 0o600)
}
