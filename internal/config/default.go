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
	"github.com/ollykeran/sshush/internal/platform"
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

// StandardConfigFile returns the absolute path to the default config file.
func StandardConfigFile() string {
	return platform.DefaultConfigPath()
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
	socketDisplay := utils.ContractHomeDirectory(platform.DefaultSocketPath())
	def := theme.DefaultTheme()
	data, err := renderDefaultConfigBytes(socketDisplay, keyPaths, def)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// CreateDefaultConfig creates the default config directory and config.toml if the file
// does not exist yet.
func CreateDefaultConfig() error {
	p := platform.DefaultConfigPath()
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	if err := WriteDefaultConfigFile(p, false); err != nil {
		return err
	}
	fmt.Println("Default config created")
	return nil
}

// AddEvalToShell appends eval $(sshush) to the preferred shell rc file (see platform.ShellRcPathForAutoSetup).
// Creates the rc file if it does not exist.
func AddEvalToShell() error {
	rcPath, ok := platform.ShellRcPathForAutoSetup()
	if !ok {
		return style.NewOutput().Error("cannot determine shell rc file (no home directory)").AsError()
	}

	if _, err := os.Stat(rcPath); os.IsNotExist(err) {
		header := "# sshush: start agent in new shells\n"
		if err := os.WriteFile(rcPath, []byte(header+platform.EvalLine), 0o644); err != nil {
			return style.NewOutput().Error("failed to create " + rcPath + ": " + err.Error()).AsError()
		}
		return nil
	} else if err != nil {
		return style.NewOutput().Error("cannot read " + rcPath + ": " + err.Error()).AsError()
	}

	f, err := os.OpenFile(rcPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return style.NewOutput().Error("failed to open " + rcPath + ": " + err.Error()).AsError()
	}
	defer f.Close()

	if _, err := f.WriteString(platform.EvalLine); err != nil {
		return style.NewOutput().Error("failed to write " + rcPath + ": " + err.Error()).AsError()
	}
	return nil
}

// SetupConfig ensures default config exists and eval is added to the shell rc if needed.
// Call from root PersistentPreRunE or main before loading config.
func SetupConfig() {
	expanded := platform.DefaultConfigPath()

	if _, err := os.Stat(expanded); os.IsNotExist(err) {
		_ = CreateDefaultConfig()
	}

	rcPath, ok := platform.ShellRcPathForAutoSetup()
	if !ok {
		return
	}
	content, err := os.ReadFile(rcPath)
	if err != nil && !os.IsNotExist(err) {
		return
	}
	if err == nil && strings.Contains(string(content), "eval $(sshush)") {
		return
	}
	_ = AddEvalToShell()
}
