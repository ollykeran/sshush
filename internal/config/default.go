package config

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/ollykeran/sshush/internal/openssh"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/theme"
	"github.com/ollykeran/sshush/internal/utils"
)

//go:embed default_config.toml.tmpl
var defaultConfigTemplateFS embed.FS

var defaultConfigTemplate = template.Must(
	template.ParseFS(defaultConfigTemplateFS, "default_config.toml.tmpl"),
)

// defaultConfigTemplateData is input for default_config.toml.tmpl.
type defaultConfigTemplateData struct {
	SocketPath   string
	KeyPathsTOML string
	ThemeText    string
	ThemeFocus   string
	ThemeAccent  string
	ThemeError   string
	ThemeWarning string
}

func findDefaultKeys() []string {
	const sshHome = "~/.ssh"

	sshPath := utils.ExpandHomeDirectory(sshHome)

	seen := make(map[string]bool)
	var paths []string

	addPath := func(p string) {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if seen[abs] {
			return
		}
		if _, err := os.Stat(abs); err != nil {
			return
		}
		seen[abs] = true
		paths = append(paths, abs)
	}

	files, err := os.ReadDir(sshPath)
	if err != nil {
		return nil
	}
	for _, f := range files {
		if f.IsDir() || strings.HasSuffix(f.Name(), ".pub") {
			continue
		}
		path := filepath.Join(sshPath, f.Name())
		if seen[path] {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			continue
		}
		if _, err := openssh.ParsePrivateKeyBlob(data); err == nil {
			addPath(path)
		}
	}
	return paths
}

// keyPathsToTOMLArray formats discovered key paths as a TOML array literal (tilde-prefixed when under $HOME).
func keyPathsToTOMLArray(keyPaths []string) string {
	if len(keyPaths) == 0 {
		return "[]"
	}
	home, _ := os.UserHomeDir()
	quoted := make([]string, len(keyPaths))
	for i, p := range keyPaths {
		if strings.HasPrefix(p, home) {
			p = "~" + strings.TrimPrefix(p, home)
		}
		quoted[i] = `"` + p + `"`
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// renderDefaultConfigBytes renders the embedded default config template. Exposed for tests.
func renderDefaultConfigBytes(socketPath string, keyPaths []string, def theme.Theme) ([]byte, error) {
	data := defaultConfigTemplateData{
		SocketPath:   socketPath,
		KeyPathsTOML: keyPathsToTOMLArray(keyPaths),
		ThemeText:    def.Text,
		ThemeFocus:   def.Focus,
		ThemeAccent:  def.Accent,
		ThemeError:   def.Error,
		ThemeWarning: def.Warning,
	}
	var buf bytes.Buffer
	if err := defaultConfigTemplate.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// StandardConfigFile returns the expanded absolute path to the default config file
// (~/.config/sshush/config.toml).
func StandardConfigFile() string {
	dir := utils.ExpandHomeDirectory("~/.config/sshush")
	return filepath.Join(dir, "config.toml")
}

// WriteDefaultConfigFile renders the default config template and writes it to path.
// Parent directories are created as needed. If overwrite is false and path already
// exists, it returns an error.
func WriteDefaultConfigFile(path string, overwrite bool) error {
	if _, err := os.Stat(path); err == nil && !overwrite {
		return style.NewOutput().
			Error("config file already exists: " + utils.DisplayPath(path)).
			Info("use --force to overwrite").
			AsError()
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	keyPaths := findDefaultKeys()
	socketPath := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "sshush.sock")
	def := theme.DefaultTheme()
	data, err := renderDefaultConfigBytes(socketPath, keyPaths, def)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// CreateDefaultConfig creates ~/.config/sshush/config.toml and writes an example
// config if neither the directory nor the file exist yet.
func CreateDefaultConfig() error {
	p := StandardConfigFile()
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	if err := WriteDefaultConfigFile(p, false); err != nil {
		return err
	}
	fmt.Println("Default config created")
	return nil
}

// AddEvalToShell appends "eval $(sshush)" to ~/.bashrc. Fails if ~/.bashrc does not exist.
func AddEvalToShell() error {
	const line = "eval $(sshush)\n"
	bashrcPath := utils.ExpandHomeDirectory("~/.bashrc")
	if _, err := os.Stat(bashrcPath); err != nil {
		return style.NewOutput().Error("~/.bashrc not found - cannot add eval $(sshush)").AsError()
	}

	bashrc, err := os.OpenFile(bashrcPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return style.NewOutput().Error("Failed to open ~/.bashrc").AsError()
	}
	defer bashrc.Close()

	bashrc.WriteString(line)
	return nil
}

// SetupConfig ensures default config exists and eval is added to bashrc if needed.
// Call from root PersistentPreRunE or main before loading config.
func SetupConfig() {
	expanded := StandardConfigFile()

	if _, err := os.Stat(expanded); os.IsNotExist(err) {
		_ = CreateDefaultConfig()
	}

	bashrcPath := utils.ExpandHomeDirectory("~/.bashrc")
	content, err := os.ReadFile(bashrcPath)
	if err == nil && !strings.Contains(string(content), "eval $(sshush)") {
		_ = AddEvalToShell()
	}
}
