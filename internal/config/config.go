package config

import (
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

// EnsureDefaultConfig creates ~/.config/sshush/ and writes an example
// config.toml if neither the directory nor the file exist yet.
func EnsureDefaultConfig(path string) {
	const exampleConfig = `# Example config.toml
socket_path = "~/.ssh/sshush.sock"
key_paths = ["~/.ssh/id_ed25519", "~/.ssh/id_rsa"]
`
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	if _, err := os.Stat(path); err == nil {
		return
	}
	os.WriteFile(path, []byte(exampleConfig), 0o644)
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
		return Config{}, style.NewOutput().Error("key_paths is required").AsError()
	}
	if cfg.SocketPath == "" {
		return Config{}, style.NewOutput().Error("socket_path is required").AsError()
	}

	return Config{
		KeyPaths:   cfg.KeyPaths,
		SocketPath: cfg.SocketPath,
	}, nil
}
