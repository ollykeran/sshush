package main

import (
	"errors"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	KeyPaths []string `toml:"key_paths"`
	SocketPath string `toml:"socket_path"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	cfg.SocketPath, _ = ExpandHomeDirectory(cfg.SocketPath)
	for i, path := range(cfg.KeyPaths) {
		cfg.KeyPaths[i], _ = ExpandHomeDirectory(path)
	}

	if cfg.KeyPaths == nil {
		return Config{}, errors.New("key_paths is required")
	}
	if cfg.SocketPath == "" {
		return Config{}, errors.New("socket_path is required")
	}

	return Config{
		KeyPaths: cfg.KeyPaths,
		SocketPath: cfg.SocketPath,
	}, nil
}