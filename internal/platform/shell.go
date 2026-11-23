package platform

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

const EvalLine = "eval $(sshush)\n"

// FishSnippet is the body of the conf.d file sshush creates for fish.
const FishSnippet = "if status is-interactive\n    sshush start --shell fish | source\nend\n"

// PowerShellSnippet is the line sshush adds to a PowerShell profile. It checks
// for sshush first, so a profile shared with a machine that lacks it stays quiet.
const PowerShellSnippet = "if (Get-Command sshush -ErrorAction SilentlyContinue) { sshush start --shell powershell | Invoke-Expression }\n"

// Shell syntaxes accepted by AuthSockLine (the --shell flag).
const (
	ShellPosix      = "posix"
	ShellFish       = "fish"
	ShellPowerShell = "powershell"
)

// ShellNames lists the --shell values for help and error messages.
const ShellNames = "posix, fish or powershell"

// ShellSetup describes where and how sshush hooks itself into shell startup.
type ShellSetup struct {
	// RcPath is the file sshush may write Snippet to.
	RcPath string
	// Snippet is the shell code that starts the agent in new shells.
	Snippet string
	// Fish is true when RcPath is a dedicated fish conf.d file rather than a shared rc file.
	Fish bool
	// PowerShell is true when RcPath is a PowerShell profile. sshush adds to one
	// that exists but never creates it: under the default execution policy a
	// profile is a script PowerShell refuses to run, with an error in every new shell.
	PowerShell bool
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
	if goruntime.GOOS == "windows" {
		profile, ok := powerShellProfilePath(parentProcessName())
		if !ok {
			return ShellSetup{}, false
		}
		return ShellSetup{RcPath: profile, Snippet: PowerShellSnippet, PowerShell: true}, true
	}
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

// PowerShellProfileStartsAgent reports whether PowerShell profile content already
// starts sshush (e.g. "sshush start --shell powershell | Invoke-Expression" or
// "sshush | iex"). Comments and other mentions of sshush do not count.
func PowerShellProfileStartsAgent(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "sshush") {
			continue
		}
		if strings.Contains(line, "invoke-expression") || strings.Contains(line, "iex") {
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

// shellFromName maps a process or path name ("-bash", "/usr/bin/fish",
// "pwsh.exe") to a shell AuthSockLine accepts, or "" if it is not a shell
// sshush knows.
func shellFromName(name string) string {
	name = strings.TrimPrefix(strings.TrimSpace(name), "-")
	// Either separator: a Windows path can turn up in $SHELL under Git Bash.
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	base := strings.TrimSuffix(strings.ToLower(name), ".exe")
	switch base {
	case "fish", "bash", "zsh", "sh":
		return base
	case "dash", "ash", "ksh", "mksh":
		return ShellPosix
	case "powershell", "pwsh":
		return ShellPowerShell
	}
	return ""
}

// DetectShell returns the shell sshush was run from, for the default
// [agent].shell: the parent process when it is a known shell, otherwise
// $SHELL, otherwise the platform's usual one (posix; PowerShell on Windows).
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
	return fallbackShell
}

// AuthSockLine returns the line that sets SSH_AUTH_SOCK to socket in the given
// shell syntax. An empty shell, "sh", "bash" and "zsh" all mean ShellPosix;
// "pwsh" means ShellPowerShell.
func AuthSockLine(shell, socket string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "", ShellPosix, "sh", "bash", "zsh":
		return "export SSH_AUTH_SOCK='" + strings.ReplaceAll(socket, "'", `'\''`) + "'", nil
	case ShellFish:
		quoted := strings.NewReplacer(`\`, `\\`, "'", `\'`).Replace(socket)
		return "set -gx SSH_AUTH_SOCK '" + quoted + "';", nil
	case ShellPowerShell, "pwsh":
		// Single quotes are literal in PowerShell; one is written by doubling it.
		return "$env:SSH_AUTH_SOCK = '" + strings.ReplaceAll(socket, "'", "''") + "'", nil
	default:
		return "", fmt.Errorf("platform: unsupported shell %q (use %s)", shell, ShellNames)
	}
}

func fileExistsRegular(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
