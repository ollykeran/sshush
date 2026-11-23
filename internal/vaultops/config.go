package vaultops

import "github.com/ollykeran/sshush/internal/config"

// EnvFromConfig is the Env a front end's verbs run in under cfg, against the
// agent at socketPath: the configured vault, and what cfg says about the agent
// there. A nil cfg names no vault and an agent this sshush manages.
//
// What only a front end knows is left for it to set: NamedVault, with VaultPath,
// when the user named a vault for one command, and AskPassphrase when it can
// prompt.
func EnvFromConfig(cfg *config.Config, socketPath string) Env {
	env := Env{SocketPath: socketPath}
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
