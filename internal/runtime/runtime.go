package runtime

import (
	"os"
	"os/exec"
	"strings"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/platform"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
)

// configPath returns the config path using the standard order (--config flag,
// default config path if it exists, SSHUSH_CONFIG, ./config.toml if it exists,
// else default path). Does not require the file to exist.
func configPath(cmd *cobra.Command) string {
	if cmd != nil && cmd.Flags().Changed("config") {
		p, _ := cmd.Flags().GetString("config")
		return utils.ExpandHomeDirectory(p)
	}
	expanded := platform.DefaultConfigPath()
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

func defaultConfigPathHuman() string {
	return utils.ContractHomeDirectory(platform.DefaultConfigPath())
}

// ResolveConfigPath returns the config file path (see configPath). The path is always
// returned; error is non-nil when the file does not exist, so callers that need the
// file must check err. Theme show/list/set can use the path regardless and handle
// missing file as needed.
func ResolveConfigPath(cmd *cobra.Command) (string, error) {
	path := configPath(cmd)
	if _, err := os.Stat(path); err != nil && os.IsNotExist(err) {
		return path, style.NewOutput().
			Error("config file not found").
			Info("create " + defaultConfigPathHuman() + " or use --config").
			AsError()
	}
	return path, nil
}

// ResolveDaemonConfigPath resolves SSHUSH_CONFIG or default daemon config path.
func ResolveDaemonConfigPath() string {
	if p := os.Getenv("SSHUSH_CONFIG"); p != "" {
		return utils.ExpandHomeDirectory(p)
	}
	return platform.DefaultConfigPath()
}

// PidFilePath returns the standard location for the sshushd pidfile.
// Uses $XDG_RUNTIME_DIR when set, otherwise the same directory as the default config (see platform.RuntimeDataDir).
func PidFilePath() string {
	return platform.DefaultPidFilePath()
}

// ResolveSocketPath returns socket path from config first, then SSH_AUTH_SOCK,
// then the default under platform.RuntimeDataDir.
func ResolveSocketPath(cfg *config.Config) (string, error) {
	if cfg != nil && strings.TrimSpace(cfg.SocketPath) != "" {
		return cfg.SocketPath, nil
	}
	if p := strings.TrimSpace(os.Getenv("SSH_AUTH_SOCK")); p != "" {
		return p, nil
	}
	return platform.DefaultSocketPath(), nil
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
