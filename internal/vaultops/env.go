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
//
// A verb works on the running agent's own vault whenever a vault agent answers:
// the agent is asked what its vault holds, so the vault file need not be
// readable from here, or even on this machine. VaultPath is what a verb falls
// back on when there is no such agent to ask, and NamedVault is how the user
// says "this file, not the agent's".
type Env struct {
	// VaultPath is [vault].vault_path or --vault-path, before ~ expansion and
	// before the directory-or-file resolution [vault.ResolveToFile] does. Empty
	// means no vault file was configured, which is fine while a vault agent is
	// there to answer for its own.
	VaultPath string

	// NamedVault reports that the user named VaultPath for this one command
	// (--vault-path) rather than it coming from config. A named vault is read
	// from its file even when a vault agent is running, since it may not be
	// that agent's vault.
	NamedVault bool

	// SocketPath is the agent socket to dial. Empty behaves as unreachable.
	SocketPath string

	// AgentVaultPath is the vault file the running agent was configured with,
	// or "" when the agent is not vault-backed or that is unknown. It matters
	// only for a named vault, where it gates the transparent unlock: naming
	// some other vault never prompts for the running agent's secret.
	AgentVaultPath string

	// AgentUnmanaged reports that this sshush does not start, stop or reload
	// the agent ([agent].type = "external"), so a remedy must not tell the user
	// to do that from here.
	AgentUnmanaged bool

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

// namedVault reports whether the user named a vault file for this command.
func (e Env) namedVault() bool { return e.NamedVault && e.VaultPath != "" }

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
