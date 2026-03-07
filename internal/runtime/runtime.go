package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
)

const defaultConfigPath = "~/.config/sshush/config.toml"

const defaultSocketFileName = "sshush.sock"
const defaultPidFileName = "sshush.pid"

// ResolveConfigPath returns a full path to a config file.
func ResolveConfigPath(cmd *cobra.Command) (string, error) {
	expanded := utils.ExpandHomeDirectory(defaultConfigPath)

	// Config loads in this order
	// config flag
	if cmd.Flags().Changed("config") {
		p, _ := cmd.Flags().GetString("config")
		return utils.ExpandHomeDirectory(p), nil
	}
	// ~/.config/sshush/config.toml
	if _, err := os.Stat(expanded); err == nil {
		return expanded, nil
	}
	// SSHUSH
	if p := os.Getenv("SSHUSH_CONFIG"); p != "" {
		return utils.ExpandHomeDirectory(p), nil
	}
	// ./config.toml
	if _, err := os.Stat("./config.toml"); err == nil {
		return "./config.toml", nil
	}
	return "", style.NewOutput().
		Error("config file not found").
		Info("create " + defaultConfigPath + " or use --config").
		AsError()
}

// ResolveDaemonConfigPath resolves SSHUSH_CONFIG or default daemon config path.
func ResolveDaemonConfigPath() string {
	if p := os.Getenv("SSHUSH_CONFIG"); p != "" {
		return utils.ExpandHomeDirectory(p)
	}
	return utils.ExpandHomeDirectory(defaultConfigPath)
}

func getXDGRuntimeDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return dir
	}
	return ""
}

// PidFilePath returns the standard location for the sshushd pidfile.
// Uses $XDG_RUNTIME_DIR/sshush.pid if available, otherwise ~/.config/sshush/sshush.pid.
func PidFilePath() string {
	runtimeDir := getXDGRuntimeDir()
	if runtimeDir != "" {
		return filepath.Join(runtimeDir, defaultPidFileName)
	}
	return utils.ExpandHomeDirectory("~/.config/sshush/sshush.pid")
}

// ResolveSocketPath returns socket path from config first, then SSH_AUTH_SOCK.
func ResolveSocketPath(cfg *config.Config) (string, error) {
	if cfg != nil && strings.TrimSpace(cfg.SocketPath) != "" {
		return cfg.SocketPath, nil
	}
	if p := strings.TrimSpace(os.Getenv("SSH_AUTH_SOCK")); p != "" {
		return p, nil
	}
	if runtimeDir := getXDGRuntimeDir(); runtimeDir != "" {
		return filepath.Join(runtimeDir, defaultSocketFileName), nil
	}
	return "", style.NewOutput().
		Error("socket path required").
		Info("export SSH_AUTH_SOCK or use --socket or --config").
		AsError()
}

// ResolveEditor returns explicit flag value, then $EDITOR, then fallback.
func ResolveEditor(editorFlag string) string {
	if strings.TrimSpace(editorFlag) != "" {
		return strings.TrimSpace(editorFlag)
	}
	if strings.TrimSpace(os.Getenv("EDITOR")) != "" {
		return strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if _, err := exec.LookPath("vim"); err == nil {
		return "vim"
	}
	if _, err := exec.LookPath("nano"); err == nil {
		return "nano"
	}
	return "vi"
}
