package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ollykeran/sshush/internal/openssh"
	"github.com/ollykeran/sshush/internal/platform"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/theme"
	"github.com/ollykeran/sshush/internal/utils"
)

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

// CreateDefaultConfig creates the default config directory and config.toml if the file
// does not exist yet.
func CreateDefaultConfig() error {
	defaultConfigDir := platform.ConfigDir()
	defaultConfigFile := platform.DefaultConfigPath()

	if err := os.MkdirAll(defaultConfigDir, 0o755); err != nil {
		return err
	}

	// Do not overwrite existing config.
	if _, err := os.Stat(defaultConfigFile); err == nil {
		return nil
	}

	// Discover keys.
	keyPaths := findDefaultKeys()

	// Convert to "~" form for nicer config.
	home, _ := os.UserHomeDir()
	quoted := make([]string, len(keyPaths))
	for i, p := range keyPaths {
		if strings.HasPrefix(p, home) {
			p = "~" + strings.TrimPrefix(p, home)
		}
		quoted[i] = `"` + p + `"`
	}

	keyPathLine := "key_paths = [" + strings.Join(quoted, ", ") + "]"

	socketDisplay := utils.ContractHomeDirectory(platform.DefaultSocketPath())
	socketPathLine := `socket_path = "` + escapeTOMLString(socketDisplay) + `"`

	def := theme.DefaultTheme()
	configLines := []string{
		"# Example config.toml",
		socketPathLine,
		keyPathLine,
		"",
		"[theme]",
		`name = "default"`,
		"# text = \"" + def.Text + "\"",
		"# focus = \"" + def.Focus + "\"",
		"# accent = \"" + def.Accent + "\"",
		"# error = \"" + def.Error + "\"",
		"# warning = \"" + def.Warning + "\"",
		"",
	}

	config := strings.Join(configLines, "\n")

	if err := os.WriteFile(defaultConfigFile, []byte(config), 0o644); err != nil {
		return err
	}

	fmt.Println("Default config created")
	return nil
}

func escapeTOMLString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
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
