package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
)

type Config struct {
	KeyPaths   []string `toml:"key_paths"`
	SocketPath string   `toml:"socket_path"`
}

func setDefaults(cfg *Config) {
	const defaultKeyPathGlob = "~/.ssh/id_*"

	cfg.SocketPath = os.Getenv("SSH_AUTH_SOCK")
	expanded := utils.ExpandHomeDirectory(defaultKeyPathGlob)
	paths, _ := filepath.Glob(expanded)
	cfg.KeyPaths = paths
}

func LoadConfig(path string) (Config, error) {
	cfg := Config{}
	setDefaults(&cfg)

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, nil // use defaults when file missing
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	cfg.SocketPath = utils.ExpandHomeDirectory(cfg.SocketPath)
	for i, path := range cfg.KeyPaths {
		cfg.KeyPaths[i] = utils.ExpandHomeDirectory(path)
	}

	if cfg.KeyPaths == nil {
		return Config{}, errors.New(style.Err("key_paths is required"))
	}
	if cfg.SocketPath == "" {
		return Config{}, errors.New(style.Err("socket_path is required"))
	}

	return Config{
		KeyPaths:   cfg.KeyPaths,
		SocketPath: cfg.SocketPath,
	}, nil
}
