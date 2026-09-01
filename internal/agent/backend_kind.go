package agent

import (
	"errors"
	"fmt"

	sshagent "golang.org/x/crypto/ssh/agent"
)

// extensionVaultLocked must match vault.ExtensionVaultLocked (package agent cannot import vault).
const extensionVaultLocked = "vault-locked"

// Backend describes the agent implementation behind a [Session].
type Backend struct {
	// Mode is "vault" or "keys".
	Mode string
	// VaultLocked reports whether the vault is locked. It is meaningful only
	// when Mode is "vault".
	VaultLocked bool
}

// Backend reports whether the agent is a vault backend or a plain keyring, and
// for a vault whether it is currently locked, in one round-trip on this
// Session's connection.
//
// An agent that answers the vault-locked extension is a vault; one that reports
// the extension unsupported — a plain keyring, or OpenSSH's own agent — is keys.
// Any other error is returned, and callers should treat it the same way they
// treat an agent they could not reach at all.
func (s *Session) Backend() (Backend, error) {
	resp, err := s.client.Extension(extensionVaultLocked, nil)
	if err == nil {
		return Backend{Mode: "vault", VaultLocked: len(resp) == 1 && resp[0] == 1}, nil
	}
	if errors.Is(err, sshagent.ErrExtensionUnsupported) {
		return Backend{Mode: "keys"}, nil
	}
	return Backend{}, fmt.Errorf("agent: probe backend: %w", err)
}
