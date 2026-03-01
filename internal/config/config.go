package config

import (
	"os"

	"github.com/BurntSushi/toml"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
)

type Config struct {
	KeyPaths   []string `toml:"key_paths"`
	SocketPath string   `toml:"socket_path"`
}

func EnsureSSHDirectory() {
	if err := os.MkdirAll(utils.ExpandHomeDirectory("~/.ssh"), 0o0700); err != nil {
		return
	}
}

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
