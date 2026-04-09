package platform

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// SocketFileName is the default Unix socket filename under the runtime data dir.
	SocketFileName = "sshush.sock"
	// PidFileName is the sshushd pidfile name under the runtime data dir.
	PidFileName = "sshush.pid"
	// ConfigFileName is the config file name inside the config directory.
	ConfigFileName = "config.toml"
)

// ConfigDir returns the absolute path to the sshush config directory:
// $XDG_CONFIG_HOME/sshush when XDG_CONFIG_HOME is set, otherwise ~/.config/sshush.
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

// DefaultSocketPath returns the default absolute path to the agent socket.
func DefaultSocketPath() string {
	return filepath.Join(RuntimeDataDir(), SocketFileName)
}

// DefaultPidFilePath returns the default absolute path to the sshushd pidfile.
func DefaultPidFilePath() string {
	return filepath.Join(RuntimeDataDir(), PidFileName)
}
