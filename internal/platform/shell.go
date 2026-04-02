package platform

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

const EvalLine = "eval $(sshush)\n"

// ShellRcPathForAutoSetup returns the shell rc file sshush may append EvalLine to.
// ok is false if there is no reasonable target (e.g. no home directory).
func ShellRcPathForAutoSetup() (rcPath string, ok bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", false
	}

	shell := strings.TrimSpace(os.Getenv("SHELL"))
	base := filepath.Base(shell)

	switch {
	case strings.Contains(base, "zsh"):
		return filepath.Join(home, ".zshrc"), true
	case strings.Contains(base, "bash"):
		return filepath.Join(home, ".bashrc"), true
	case goruntime.GOOS == "darwin":
		// Default login shell on macOS is zsh; SHELL may be empty in some contexts.
		return filepath.Join(home, ".zshrc"), true
	default:
		bashRC := filepath.Join(home, ".bashrc")
		zshRC := filepath.Join(home, ".zshrc")
		if fileExistsRegular(bashRC) {
			return bashRC, true
		}
		if fileExistsRegular(zshRC) {
			return zshRC, true
		}
		// Prefer bashrc on non-macOS when neither exists (common Linux default).
		return bashRC, true
	}
}

func fileExistsRegular(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
