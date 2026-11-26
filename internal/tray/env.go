package tray

import (
	"os"
	"path/filepath"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/vaultops"
)

// LoadConfig reads the config file the CLI would use with no --config flag. A
// missing file is not an error: the tray then targets the default socket.
func LoadConfig() (*config.Config, error) {
	path := runtime.ConfigPathDefault()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &config.Config{}, nil
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// defaultVaultPath is where a vault goes when config names none: beside the
// config file.
func defaultVaultPath() string {
	return filepath.Join(filepath.Dir(runtime.ConfigPathDefault()), "vault.json")
}

// vaultEnv is env with the vault path defaulted, for the Vault page's verbs,
// which look after the vault whether or not the agent serves it, and whether
// or not config names one yet.
func vaultEnv(env vaultops.Env) vaultops.Env {
	if env.VaultPath == "" {
		env.VaultPath = defaultVaultPath()
	}
	return env
}
