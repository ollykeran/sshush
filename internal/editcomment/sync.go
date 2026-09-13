package editcomment

import (
	"fmt"
	"os"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/vault"
	ssh "golang.org/x/crypto/ssh"
)

// SyncResult reports what SyncAgent attempted and any resulting errors.
type SyncResult struct {
	Reloaded    bool
	ReloadErr   error
	VaultSynced bool
	VaultErr    error
}

// SyncAgent reloads fingerprint's identity in session, if it is currently
// loaded, then -- for vault-backed agents -- persists comment as the
// identity's vault-stored comment. privateKeyPath must already have been
// rewritten on disk with comment before calling this.
//
// Reload runs before the vault write so comment, not whatever the reload
// path re-derives by re-parsing the key file, is always the value the vault
// ends up storing. The two steps are independent and both attempted even if
// one fails, so callers can report them as separate non-fatal warnings.
//
// A nil session, or one whose backend can't be determined, means there is
// nothing to sync; SyncAgent returns a zero SyncResult in that case.
func SyncAgent(session *agent.Session, fingerprint, privateKeyPath, comment string) SyncResult {
	var result SyncResult
	if session == nil {
		return result
	}

	backend, err := session.Backend()
	if err != nil {
		return result
	}

	if isKeyLoadedInAgent(session, fingerprint) {
		result.ReloadErr = reloadKeyInAgent(session, backend, privateKeyPath)
		result.Reloaded = result.ReloadErr == nil
	}

	// Only existing identities are updated.
	if backend.Mode == "vault" {
		result.VaultErr = vault.SetComment(session, fingerprint, comment)
		result.VaultSynced = result.VaultErr == nil
	}

	return result
}

// isKeyLoadedInAgent reports whether a key with the given fingerprint is loaded
// in the running agent. A nil session means the agent is not reachable.
func isKeyLoadedInAgent(session *agent.Session, fingerprint string) bool {
	if session == nil {
		return false
	}
	agentKeys, err := session.List()
	if err != nil {
		return false
	}
	for _, k := range agentKeys {
		pub, err := ssh.ParsePublicKey(k.Blob)
		if err != nil {
			continue
		}
		if ssh.FingerprintSHA256(pub) == fingerprint {
			return true
		}
	}
	return false
}

// reloadKeyInAgent removes the old key and re-adds it after an edit.
// For vault mode, uses add-key-opts; for standard mode, removes and re-adds.
// A nil session means the agent is not reachable and there is nothing to reload.
func reloadKeyInAgent(session *agent.Session, backend agent.Backend, privateKeyPath string) error {
	if session == nil {
		return nil
	}

	// Find and remove the old key. The key file's fingerprint does not change
	// per agent key, so resolve it once rather than inside the loop.
	agentKeys, err := session.List()
	if err != nil {
		return fmt.Errorf("list keys: %w", err)
	}
	existingFP := ""
	if _, statErr := os.Stat(privateKeyPath); statErr == nil {
		if pubKey, _, _, parseErr := agent.ParseKeyFromPath(privateKeyPath); parseErr == nil {
			existingFP = ssh.FingerprintSHA256(pubKey)
		}
	}
	if existingFP != "" {
		for _, k := range agentKeys {
			pub, parseErr := ssh.ParsePublicKey(k.Blob)
			if parseErr != nil {
				continue
			}
			if ssh.FingerprintSHA256(pub) == existingFP {
				_ = session.Remove(pub)
				break
			}
		}
	}

	// Re-add the key with updated comment
	if backend.Mode == "vault" {
		if err := vault.AddPrivateKeyFile(session, privateKeyPath, true); err != nil {
			return fmt.Errorf("reload key in vault: %w", err)
		}
	} else {
		if err := session.AddKeyFromPath(privateKeyPath); err != nil {
			return fmt.Errorf("reload key in agent: %w", err)
		}
	}
	return nil
}
