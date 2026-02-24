package utils

import (
	"os"
	"strings"

	"github.com/ollykeran/sshush/internal/style"
	"github.com/spf13/cobra"
)

func ResolveConfigPath(cmd *cobra.Command) (string, error) {
	/* Returns a full path to a config file that exists.
	 */
	const defaultConfigPath = "~/.config/sshush/config.toml"
	expanded := ExpandHomeDirectory(defaultConfigPath)

	if cmd.Flags().Changed("config") {
		p, _ := cmd.Flags().GetString("config")
		return ExpandHomeDirectory(p), nil
	}
	if p := os.Getenv("SSHUSH_CONFIG"); p != "" {
		return ExpandHomeDirectory(p), nil
	}
	if _, err := os.Stat(expanded); err == nil {
		return expanded, nil
	}
	if _, err := os.Stat("./config.toml"); err == nil {
		return "./config.toml", nil
	}
	return "", style.NewOutput().
		Error("config file not found").
		Info("create " + defaultConfigPath + " or use --config").
		AsError()
}

// PidFilePath returns the standard location for the sshushd pidfile.
// Uses $XDG_RUNTIME_DIR/sshush.pid if available, otherwise ~/.config/sshush/sshush.pid.
func PidFilePath() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return dir + "/sshush.pid"
	}
	return ExpandHomeDirectory("~/.config/sshush/sshush.pid")
}

func ExpandHomeDirectory(path string) string {
	if strings.Contains(path, "~") {
		home_dir, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return strings.ReplaceAll(path, "~", home_dir)
	}
	return path
}

// func ExpandRelativePath(path string) (string, error) {
// 	if strings.HasPrefix(path, "./") {
// 		cwd, err := os.Getwd()
// 		if err != nil {
// 			return "", err
// 		}
// 		return strings.ReplaceAll(path, "./", cwd), nil
// 	}
// 	return path, nil
// }
