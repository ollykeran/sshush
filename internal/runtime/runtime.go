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
const defaultServerPidFileName = "sshush-server.pid"

// configPath returns the config path using the standard order (--config flag,
// ~/.config/sshush/config.toml if it exists, SSHUSH_CONFIG, ./config.toml if it exists,
// else default path). Does not require the file to exist.
func configPath(cmd *cobra.Command) string {
	if cmd != nil && cmd.Flags().Changed("config") {
		p, _ := cmd.Flags().GetString("config")
		return utils.ExpandHomeDirectory(p)
	}
	expanded := utils.ExpandHomeDirectory(defaultConfigPath)
	if _, err := os.Stat(expanded); err == nil {
		return expanded
	}
	if p := os.Getenv("SSHUSH_CONFIG"); p != "" {
		return utils.ExpandHomeDirectory(p)
	}
	if _, err := os.Stat("./config.toml"); err == nil {
		return "./config.toml"
	}
	return expanded
}

// ResolveConfigPath returns the config file path (see configPath). The path is always
// returned; error is non-nil when the file does not exist, so callers that need the
// file must check err. Theme show/list/set can use the path regardless and handle
// missing file as needed.
func ResolveConfigPath(cmd *cobra.Command) (string, error) {
	path := configPath(cmd)
	if _, err := os.Stat(path); err != nil && os.IsNotExist(err) {
		return path, style.NewOutput().
			Error("config file not found: " + utils.DisplayPath(path)).
			Info("create " + defaultConfigPath + " or use --config").
			AsError()
	}
	return path, nil
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

// ServerPidFilePath returns the standard location for the SSH server daemon pidfile.
// Uses $XDG_RUNTIME_DIR/sshush-server.pid if available, otherwise ~/.config/sshush/sshush-server.pid.
func ServerPidFilePath() string {
	runtimeDir := getXDGRuntimeDir()
	if runtimeDir != "" {
		return filepath.Join(runtimeDir, defaultServerPidFileName)
	}
	return utils.ExpandHomeDirectory("~/.config/sshush/sshush-server.pid")
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
