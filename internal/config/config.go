package config

import (
	"os"

	"github.com/BurntSushi/toml"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
)

// Config holds socket path and key paths from the TOML config file.
type Config struct {
	KeyPaths   []string `toml:"key_paths"`   // Paths to private keys to load into the agent.
	SocketPath string   `toml:"socket_path"` // Unix socket path for the agent.
}

// EnsureSSHDirectory creates ~/.ssh with mode 0700 if it does not exist.
func EnsureSSHDirectory() {
	if err := os.MkdirAll(utils.ExpandHomeDirectory("~/.ssh"), 0o0700); err != nil {
		return
	}
}

// LoadConfig reads and parses a TOML config file. Paths are expanded (~).
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	cfg.SocketPath = utils.ExpandHomeDirectory(cfg.SocketPath)
	for i, p := range cfg.KeyPaths {
		cfg.KeyPaths[i] = utils.ExpandHomeDirectory(p)
	}

	if cfg.KeyPaths == nil {
		return Config{}, style.NewOutput().Error("key_paths is required").AsError()
	}
	if cfg.SocketPath == "" {
		return Config{}, style.NewOutput().Error("socket_path is required").AsError()
	}

	return cfg, nil
}
