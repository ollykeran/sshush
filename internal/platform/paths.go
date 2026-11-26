package platform

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

const (
	// SocketFileName is the default Unix socket filename under the runtime data dir.
	SocketFileName = "sshush.sock"
	// WindowsPipeName is the default agent endpoint on Windows: the named pipe
	// of Windows' own OpenSSH agent, which ssh.exe uses when SSH_AUTH_SOCK is
	// unset, so ssh finds sshush with no setup at all. It is one name for the
	// whole machine, and only one agent can hold it: the Windows "OpenSSH
	// Authentication Agent" service has to be stopped for sshush to serve it.
	WindowsPipeName = OpenSSHAgentPipeName
	// OpenSSHAgentPipeName is the pipe Windows' OpenSSH agent serves.
	OpenSSHAgentPipeName = `\\.\pipe\openssh-ssh-agent`
	// SSHushPipeName is a pipe only sshush uses, for when Windows' own agent or
	// another one needs OpenSSHAgentPipeName.
	SSHushPipeName = `\\.\pipe\sshush-agent`
	// OpenSSHAgentService is the service name of Windows' own agent, shown in
	// the Services list as "OpenSSH Authentication Agent".
	OpenSSHAgentService = "ssh-agent"
	// PidFileName is the sshushd pidfile name under the runtime data dir.
	PidFileName = "sshush.pid"
	// ConfigFileName is the config file name inside the config directory.
	ConfigFileName = "config.toml"
	// ServerHostKeyFileName is the SSH server's host key file inside the config directory.
	ServerHostKeyFileName = "server_host_ed25519"
	// ServerLogFileName is the SSH server's log file name.
	ServerLogFileName = "server.log"
)

// ConfigDir returns the absolute path to the sshush config directory:
// $XDG_CONFIG_HOME/sshush when XDG_CONFIG_HOME is set, otherwise ~/.config/sshush,
// on Windows as everywhere else.
func ConfigDir() string {
	if d := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); d != "" {
		return filepath.Join(d, "sshush")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".config", "sshush")
	}
	return filepath.Join(home, ".config", "sshush")
}

// DefaultConfigPath returns the absolute path to the default config file.
func DefaultConfigPath() string {
	return filepath.Join(ConfigDir(), ConfigFileName)
}

// RuntimeDataDir returns the directory for the agent socket and pidfile.
// Uses $XDG_RUNTIME_DIR when set (typical on Linux desktop sessions).
// Otherwise falls back to ConfigDir so paths stay absolute without XDG (typical on macOS).
func RuntimeDataDir() string {
	if d := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); d != "" {
		return d
	}
	return ConfigDir()
}

// CygwinSocketFileName is the default name of the socket file sshushd
// writes on Windows for Cygwin and MSYS2 programs (Git Bash's ssh).
const CygwinSocketFileName = "cygwin-agent.sock"

// DefaultCygwinSocketPath is where sshushd writes that file unless
// [agent].cygwin_socket says otherwise.
func DefaultCygwinSocketPath() string {
	return filepath.Join(RuntimeDataDir(), CygwinSocketFileName)
}

// DefaultSocketPath returns the default absolute path to the agent socket, or
// on Windows the agent's named pipe.
func DefaultSocketPath() string {
	if goruntime.GOOS == "windows" {
		return WindowsPipeName
	}
	return filepath.Join(RuntimeDataDir(), SocketFileName)
}

// IsOpenSSHAgentPipe reports whether path names OpenSSHAgentPipeName. Windows
// ignores case in pipe names and takes either slash, so it can be written
// several ways.
func IsOpenSSHAgentPipe(path string) bool {
	return strings.EqualFold(strings.ReplaceAll(strings.TrimSpace(path), "/", `\`), OpenSSHAgentPipeName)
}

// WindowsAgentHoldsPipe reports whether path is OpenSSHAgentPipeName while
// Windows' own agent service is running, which means the pipe is the
// service's and not sshush's: the service creates it as it starts.
func WindowsAgentHoldsPipe(path string) bool {
	return IsOpenSSHAgentPipe(path) && WindowsServiceRunning(OpenSSHAgentService)
}

// DefaultPidFilePath returns the default absolute path to the sshushd pidfile.
func DefaultPidFilePath() string {
	return filepath.Join(RuntimeDataDir(), PidFileName)
}

// ServerHostKeyPath returns where the SSH server's host key lives: the configured
// path when one is set, otherwise a stable file in the config directory. It goes
// there rather than in the runtime dir because a host key that did not survive a
// reboot would greet returning clients with a host-key-changed warning.
func ServerHostKeyPath(configured string) string {
	if p := strings.TrimSpace(configured); p != "" {
		return p
	}
	return filepath.Join(ConfigDir(), ServerHostKeyFileName)
}

// ServerLogPath returns where the SSH server writes its log: the configured path
// when one is set, otherwise server.log under $XDG_STATE_HOME/sshush — XDG's home
// for logs — when that is set, and in the config directory beside the host key
// when it is not. Like the host key, it stays out of the runtime dir, which would
// lose the log at every reboot.
func ServerLogPath(configured string) string {
	if p := strings.TrimSpace(configured); p != "" {
		return p
	}
	if d := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); d != "" {
		return filepath.Join(d, "sshush", ServerLogFileName)
	}
	return filepath.Join(ConfigDir(), ServerLogFileName)
}
