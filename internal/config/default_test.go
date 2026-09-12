package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/ollykeran/sshush/internal/platform"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/theme"
	"github.com/ollykeran/sshush/internal/utils"
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

// serverOptionKeys lists every [server] key the config file understands, read off
// serverSection's toml tags, so an option added there without a line in the
// default config fails the tests below.
func serverOptionKeys() []string {
	typ := reflect.TypeOf(serverSection{})
	keys := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		keys = append(keys, typ.Field(i).Tag.Get("toml"))
	}
	return keys
}

// loadRenderedConfig writes data to a file and loads it as a config.
func loadRenderedConfig(t *testing.T, data []byte) Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v\n%s", err, string(data))
	}
	return cfg
}

// TestRenderDefaultConfigBytes_listsEveryServerOptionCommentedOut checks the
// default config documents every [server] option while enabling none of them:
// running the server has to be a deliberate edit.
func TestRenderDefaultConfigBytes_listsEveryServerOptionCommentedOut(t *testing.T) {
	t.Parallel()
	data, err := renderDefaultConfigBytes("/run/user/1000/sshush.sock", nil, theme.DefaultTheme())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range serverOptionKeys() {
		if !regexp.MustCompile(`(?m)^# ` + regexp.QuoteMeta(key) + ` = `).Match(data) {
			t.Errorf("default config has no commented-out %q line", key+" = ")
		}
	}
	if !regexp.MustCompile(`(?m)^# \[server\]$`).Match(data) {
		t.Error("default config has no commented-out [server] header")
	}

	cfg := loadRenderedConfig(t, data)
	if cfg.ServerListenPort != 0 || cfg.ServerAuthorizedKeys != "" || cfg.ServerHostKey != "" ||
		cfg.ServerShell != "" || cfg.ServerPasswordAuth {
		t.Errorf("default config sets server options: %+v", cfg)
	}
}

// TestRenderDefaultConfigBytes_serverOptionsLoadOnceUncommented checks the
// commented-out values are valid as written — the right TOML types, and the
// defaults they claim to be.
func TestRenderDefaultConfigBytes_serverOptionsLoadOnceUncommented(t *testing.T) {
	t.Parallel()
	data, err := renderDefaultConfigBytes("/run/user/1000/sshush.sock", nil, theme.DefaultTheme())
	if err != nil {
		t.Fatal(err)
	}
	quoted := make([]string, 0, len(serverOptionKeys()))
	for _, key := range serverOptionKeys() {
		quoted = append(quoted, regexp.QuoteMeta(key))
	}
	uncomment := regexp.MustCompile(`^# (\[server\]|(?:` + strings.Join(quoted, "|") + `) = .*)$`)
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if m := uncomment.FindStringSubmatch(line); m != nil {
			lines[i] = m[1]
		}
	}

	cfg := loadRenderedConfig(t, []byte(strings.Join(lines, "\n")))
	if cfg.ServerListenPort != 2222 {
		t.Errorf("ServerListenPort: got %d, want 2222", cfg.ServerListenPort)
	}
	if want := platform.ServerHostKeyPath(""); cfg.ServerHostKey != want {
		t.Errorf("ServerHostKey: got %q, want the real default %q", cfg.ServerHostKey, want)
	}
	if cfg.ServerAuthorizedKeys == "" || cfg.ServerShell == "" {
		t.Errorf("authorized_keys/shell examples did not load: %q, %q", cfg.ServerAuthorizedKeys, cfg.ServerShell)
	}
	if cfg.ServerPasswordAuth {
		t.Error("password_auth should be shown at its default, false")
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
	var doc struct {
		Agent struct {
			SocketPath string `toml:"socket_path"`
		} `toml:"agent"`
	}
	if _, err := toml.Decode(string(data), &doc); err != nil {
		t.Fatal(err)
	}
	sp := doc.Agent.SocketPath
	if sp == "" {
		t.Fatal("empty [agent].socket_path")
	}
	if !strings.HasPrefix(sp, "~") {
		t.Fatalf("expected contracted socket_path under home, got %q", sp)
	}
	expanded := utils.ExpandHomeDirectory(sp)
	if !filepath.IsAbs(expanded) {
		t.Fatalf("expanded socket_path not absolute: %q", expanded)
	}
}
