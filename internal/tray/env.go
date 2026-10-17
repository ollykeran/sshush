package tray

import (
	"os"

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

// EnvFor builds the vaultops.Env for the tray's verbs from cfg and the socket
// it polls.
//
// SPIKE: copied from the unexported vaultEnv in internal/cli/vault.go, minus
// the --vault-path flag. AskPassphrase stays nil: the tray prompts with its own
// dialog before calling vaultops, never from inside a verb. If the tray
// survives the spike, this belongs in a package both front ends share.
func EnvFor(cfg *config.Config, socketPath string) vaultops.Env {
	env := vaultops.Env{SocketPath: socketPath}
	if cfg == nil {
		return env
	}
	env.VaultPath = cfg.VaultPath
	if cfg.IsVault() {
		env.AgentVaultPath = cfg.VaultPath
	}
	env.AgentUnmanaged = cfg.IsExternal()
	env.AgentElsewhere = !cfg.OwnsAgentKeys() && cfg.AgentType != ""
	return env
}
