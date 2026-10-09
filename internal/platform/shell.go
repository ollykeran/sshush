package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
)

const EvalLine = "eval $(sshush)\n"

// FishSnippet is the body of the conf.d file sshush creates for fish.
const FishSnippet = "if status is-interactive\n    sshush start --shell fish | source\nend\n"

// Shell syntaxes accepted by AuthSockLine (the --shell flag).
const (
	ShellPosix = "posix"
	ShellFish  = "fish"
)

// ShellSetup describes where and how sshush hooks itself into shell startup.
type ShellSetup struct {
	// RcPath is the file sshush may write Snippet to.
	RcPath string
	// Snippet is the shell code that starts the agent in new shells.
	Snippet string
	// Fish is true when RcPath is a dedicated fish conf.d file rather than a shared rc file.
	Fish bool
}

// FishConfigDir returns the fish config directory:
// $XDG_CONFIG_HOME/fish when XDG_CONFIG_HOME is set, otherwise ~/.config/fish.
func FishConfigDir(home string) string {
	if d := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); d != "" {
		return filepath.Join(d, "fish")
	}
	return filepath.Join(home, ".config", "fish")
}

// ShellSetupForAutoSetup returns the shell startup file sshush may add its snippet to.
// ok is false if there is no reasonable target (e.g. no home directory).
func ShellSetupForAutoSetup() (setup ShellSetup, ok bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ShellSetup{}, false
	}

	shell := strings.TrimSpace(os.Getenv("SHELL"))
	base := filepath.Base(shell)

	rc := func(name string) (ShellSetup, bool) {
		return ShellSetup{RcPath: filepath.Join(home, name), Snippet: EvalLine}, true
	}

	switch {
	case strings.Contains(base, "zsh"):
		return rc(".zshrc")
	case strings.Contains(base, "bash"):
		return rc(".bashrc")
	case strings.Contains(base, "fish"):
		return ShellSetup{
			RcPath:  filepath.Join(FishConfigDir(home), "conf.d", "sshush.fish"),
			Snippet: FishSnippet,
			Fish:    true,
		}, true
	case goruntime.GOOS == "darwin":
		// Default login shell on macOS is zsh; SHELL may be empty in some contexts.
		return rc(".zshrc")
	default:
		if fileExistsRegular(filepath.Join(home, ".bashrc")) {
			return rc(".bashrc")
		}
		if fileExistsRegular(filepath.Join(home, ".zshrc")) {
			return rc(".zshrc")
		}
		// Prefer bashrc on non-macOS when neither exists (common Linux default).
		return rc(".bashrc")
	}
}

// ShellRcPathForAutoSetup returns the shell rc file sshush may add its startup snippet to.
// ok is false if there is no reasonable target (e.g. no home directory).
func ShellRcPathForAutoSetup() (rcPath string, ok bool) {
	setup, ok := ShellSetupForAutoSetup()
	return setup.RcPath, ok
}

// FishConfigStartsAgent reports whether fish config content already starts sshush
// (e.g. "eval (sshush start)" or "sshush | source"). Comments and the completion
// line ("sshush completion fish | source") do not count.
func FishConfigStartsAgent(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "sshush") || strings.Contains(line, "completion") {
			continue
		}
		if strings.Contains(line, "eval") || strings.Contains(line, "source") {
			return true
		}
	}
	return false
}

// ValidShell reports whether shell is a value AuthSockLine accepts
// ([agent].shell in config, --shell on the command line).
func ValidShell(shell string) bool {
	_, err := AuthSockLine(shell, "")
	return err == nil
}

// shellFromName maps a process or path name ("-bash", "/usr/bin/fish") to a
// shell AuthSockLine accepts, or "" if it is not a shell sshush knows.
func shellFromName(name string) string {
	base := strings.ToLower(filepath.Base(strings.TrimPrefix(strings.TrimSpace(name), "-")))
	switch base {
	case "fish", "bash", "zsh", "sh":
		return base
	case "dash", "ash", "ksh", "mksh":
		return ShellPosix
	}
	return ""
}

// parentProcessName returns the name of the process that ran sshush, or "".
func parentProcessName() string {
	ppid := strconv.Itoa(os.Getppid())
	if data, err := os.ReadFile(filepath.Join("/proc", ppid, "comm")); err == nil {
		return strings.TrimSpace(string(data))
	}
	out, err := exec.Command("ps", "-o", "comm=", "-p", ppid).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// DetectShell returns the shell sshush was run from, for the default
// [agent].shell: the parent process when it is a known shell, otherwise
// $SHELL, otherwise ShellPosix.
func DetectShell() string {
	return detectShell(parentProcessName(), os.Getenv("SHELL"))
}

func detectShell(parentName, shellEnv string) string {
	if s := shellFromName(parentName); s != "" {
		return s
	}
	if s := shellFromName(shellEnv); s != "" {
		return s
	}
	return ShellPosix
}

// AuthSockLine returns the line that sets SSH_AUTH_SOCK to socket in the given
// shell syntax. An empty shell, "sh", "bash" and "zsh" all mean ShellPosix.
func AuthSockLine(shell, socket string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "", ShellPosix, "sh", "bash", "zsh":
		return "export SSH_AUTH_SOCK='" + strings.ReplaceAll(socket, "'", `'\''`) + "'", nil
	case ShellFish:
		quoted := strings.NewReplacer(`\`, `\\`, "'", `\'`).Replace(socket)
		return "set -gx SSH_AUTH_SOCK '" + quoted + "';", nil
	default:
		return "", fmt.Errorf("platform: unsupported shell %q (use posix or fish)", shell)
	}
}

func fileExistsRegular(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
