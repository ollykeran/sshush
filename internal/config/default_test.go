package config

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
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
	if runtime.GOOS == "windows" {
		t.Skip("Unix path layout")
	}
	t.Parallel()
	data, err := renderDefaultConfigBytes("/run/user/1000/sshush.sock", []string{"/tmp/id_ed25519"}, "fish", theme.DefaultTheme())
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
	if cfg.Shell != "fish" {
		t.Errorf("Shell: got %q, want the shell passed at render time", cfg.Shell)
	}
}

func TestLoadConfig_shell(t *testing.T) {
	tests := []struct {
		name, line, want string
		wantErr          bool
	}{
		{"omitted means default", "", "", false},
		{"fish", "shell = \"fish\"\n", "fish", false},
		{"bash alias", "shell = \"bash\"\n", "bash", false},
		{"powershell", "shell = \"powershell\"\n", "powershell", false},
		{"unsupported", "shell = \"nushell\"\n", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			content := "[agent]\nsocket_path = \"/tmp/a.sock\"\ntype = \"keys\"\nkey_paths = []\n" + tt.line
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig(path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error for unsupported [agent].shell")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Shell != tt.want {
				t.Fatalf("Shell: got %q, want %q", cfg.Shell, tt.want)
			}
		})
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
	data, err := renderDefaultConfigBytes("/run/user/1000/sshush.sock", nil, "posix", theme.DefaultTheme())
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
	data, err := renderDefaultConfigBytes("/run/user/1000/sshush.sock", nil, "posix", theme.DefaultTheme())
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
	if runtime.GOOS == "windows" {
		t.Skip("Unix path layout")
	}
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
	if runtime.GOOS == "windows" {
		t.Skip("Unix path layout")
	}
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
	if runtime.GOOS == "windows" {
		t.Skip("Unix path layout")
	}
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

// setupFishHome points HOME at a temp dir with SHELL=fish and an existing
// sshush config, so SetupConfig only exercises the shell startup snippet.
func setupFishHome(t *testing.T) (home, fishDir string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/usr/bin/fish")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	if err := os.MkdirAll(filepath.Join(home, ".config", "sshush"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "sshush", "config.toml"), []byte("[agent]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home, filepath.Join(home, ".config", "fish")
}

func TestSetupConfig_fishWritesConfD(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shells; Windows sets up a PowerShell profile")
	}
	home, fishDir := setupFishHome(t)

	SetupConfig()

	snippetPath := filepath.Join(fishDir, "conf.d", "sshush.fish")
	data, err := os.ReadFile(snippetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "sshush start --shell fish | source") {
		t.Fatalf("unexpected snippet: %q", data)
	}
	for _, rc := range []string{".bashrc", ".zshrc"} {
		if _, err := os.Stat(filepath.Join(home, rc)); !os.IsNotExist(err) {
			t.Fatalf("%s should not be created for fish", rc)
		}
	}

	// Second run must not append to or rewrite the snippet.
	SetupConfig()
	again, err := os.ReadFile(snippetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) {
		t.Fatalf("snippet changed on second run: %q", again)
	}
}

func TestSetupConfig_fishRespectsExistingConfigFish(t *testing.T) {
	_, fishDir := setupFishHome(t)
	if err := os.MkdirAll(fishDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configFish := "if status is-interactive\n  sshush completion fish | source\n  eval (sshush start)\nend\n"
	if err := os.WriteFile(filepath.Join(fishDir, "config.fish"), []byte(configFish), 0o644); err != nil {
		t.Fatal(err)
	}

	SetupConfig()

	if _, err := os.Stat(filepath.Join(fishDir, "conf.d", "sshush.fish")); !os.IsNotExist(err) {
		t.Fatal("conf.d/sshush.fish should not be created when config.fish already starts sshush")
	}
}

func TestSetupConfig_fishCompletionLineAloneIsNotSetup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shells; Windows sets up a PowerShell profile")
	}
	_, fishDir := setupFishHome(t)
	if err := os.MkdirAll(fishDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fishDir, "config.fish"), []byte("sshush completion fish | source\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	SetupConfig()

	if _, err := os.Stat(filepath.Join(fishDir, "conf.d", "sshush.fish")); err != nil {
		t.Fatalf("expected conf.d/sshush.fish: %v", err)
	}
}

func TestKeyPathsToTOMLArray_backslashes(t *testing.T) {
	// A Windows path must not put backslashes in a TOML basic string, where they are escapes.
	got := keyPathsToTOMLArray([]string{filepath.FromSlash("/keys/id_ed25519")})
	if strings.Contains(got, `\`) {
		t.Fatalf("got %s, want forward slashes only", got)
	}
}

func TestSetupPowerShellProfile(t *testing.T) {
	setup := func(t *testing.T) platform.ShellSetup {
		return platform.ShellSetup{
			RcPath:     filepath.Join(t.TempDir(), "Microsoft.PowerShell_profile.ps1"),
			Snippet:    platform.PowerShellSnippet,
			PowerShell: true,
		}
	}

	t.Run("no profile: hint on first run, nothing created", func(t *testing.T) {
		s := setup(t)
		var hint bytes.Buffer
		setupPowerShellProfile(s, true, &hint)
		if _, err := os.Stat(s.RcPath); !os.IsNotExist(err) {
			t.Fatalf("profile must not be created, stat err = %v", err)
		}
		if !strings.Contains(hint.String(), "Invoke-Expression") {
			t.Fatalf("expected a hint naming the line to add, got %q", hint.String())
		}
	})

	t.Run("no profile: silent after the first run", func(t *testing.T) {
		var hint bytes.Buffer
		setupPowerShellProfile(setup(t), false, &hint)
		if hint.Len() != 0 {
			t.Fatalf("expected no hint, got %q", hint.String())
		}
	})

	t.Run("existing profile gets the line once", func(t *testing.T) {
		s := setup(t)
		if err := os.WriteFile(s.RcPath, []byte("Set-Alias ll ls"), 0o644); err != nil {
			t.Fatal(err)
		}
		var hint bytes.Buffer
		setupPowerShellProfile(s, true, &hint)
		setupPowerShellProfile(s, false, &hint)
		data, err := os.ReadFile(s.RcPath)
		if err != nil {
			t.Fatal(err)
		}
		got := string(data)
		if strings.Count(got, platform.PowerShellSnippet) != 1 {
			t.Fatalf("expected the snippet exactly once, got:\n%s", got)
		}
		if !strings.HasPrefix(got, "Set-Alias ll ls\n") {
			t.Fatalf("existing content must be kept on its own line, got:\n%s", got)
		}
		if hint.Len() != 0 {
			t.Fatalf("expected no hint, got %q", hint.String())
		}
	})

	t.Run("hand-written start line is respected", func(t *testing.T) {
		s := setup(t)
		own := "sshush | iex\n"
		if err := os.WriteFile(s.RcPath, []byte(own), 0o644); err != nil {
			t.Fatal(err)
		}
		setupPowerShellProfile(s, true, &bytes.Buffer{})
		data, _ := os.ReadFile(s.RcPath)
		if string(data) != own {
			t.Fatalf("profile changed:\n%s", data)
		}
	})
}
