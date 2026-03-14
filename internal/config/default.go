package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ollykeran/sshush/internal/openssh"
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

// CreateDefaultConfig creates ~/.config/sshush/config.toml and writes an example
// config if neither the directory nor the file exist yet.
func CreateDefaultConfig() error {
	const defaultConfigDir = "~/.config/sshush"
	const defaultConfigFileName = "config.toml"

	defaultConfigDirExpanded := utils.ExpandHomeDirectory(defaultConfigDir)
	defaultConfigFile := filepath.Join(defaultConfigDirExpanded, defaultConfigFileName)

	if err := os.MkdirAll(defaultConfigDirExpanded, 0o755); err != nil {
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

	// Socket path
	socketPath := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "sshush.sock")
	socketPathLine := `socket_path = "` + socketPath + `"`

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
	const defaultConfigPath = "~/.config/sshush/config.toml"
	expanded := utils.ExpandHomeDirectory(defaultConfigPath)

	if _, err := os.Stat(expanded); os.IsNotExist(err) {
		_ = CreateDefaultConfig()
	}

	bashrcPath := utils.ExpandHomeDirectory("~/.bashrc")
	content, err := os.ReadFile(bashrcPath)
	if err == nil && !strings.Contains(string(content), "eval $(sshush)") {
		_ = AddEvalToShell()
	}
}
