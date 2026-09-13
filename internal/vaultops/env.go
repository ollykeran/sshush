package vaultops

import (
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
)

// PassphraseFunc reads a passphrase from the user, prompting with prompt. The
// bytes it returns belong to this package, which zeroes them after use.
type PassphraseFunc func(prompt string) ([]byte, error)

// Env names the vault and the agent a verb works against, and says whether the
// front end can ask the user for a passphrase.
type Env struct {
	// VaultPath is [vault].vault_path or --vault-path, before ~ expansion and
	// before the directory-or-file resolution [vault.ResolveToFile] does. Empty
	// means no vault was configured; only the verbs that talk purely to the
	// agent tolerate that.
	VaultPath string

	// SocketPath is the agent socket to dial. Empty behaves as unreachable.
	SocketPath string

	// AgentVaultPath is the vault file the running agent was configured with,
	// or "" when the agent is not vault-backed or that is unknown. It gates the
	// transparent unlock: a passphrase is only ever asked for on an agent that
	// serves the very vault being operated on, so listing some other vault
	// never prompts for the running agent's secret.
	AgentVaultPath string

	// AskPassphrase prompts for the agent's unlock passphrase. Nil means this
	// front end cannot prompt — the TUI drives its passphrase modal from its
	// own event loop and cannot block inside a tea.Cmd — and a locked agent is
	// then reported rather than unlocked.
	AskPassphrase PassphraseFunc
}

// vaultFile returns VaultPath expanded and resolved to the vault JSON file, or
// "" when no vault path was configured.
func (e Env) vaultFile() string { return resolveVaultPath(e.VaultPath) }

// agentVaultFile is vaultFile for the vault the running agent serves.
func (e Env) agentVaultFile() string { return resolveVaultPath(e.AgentVaultPath) }

func resolveVaultPath(path string) string {
	if path == "" {
		return ""
	}
	return vault.ResolveToFile(utils.ExpandHomeDirectory(path))
}

// zero overwrites b, for passphrase material this package obtained itself.
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
