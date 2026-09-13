package agent

import (
	"errors"
	"fmt"
)

// Backend describes the agent implementation behind a [Session].
type Backend struct {
	// Mode is "vault" or "keys".
	Mode string
	// VaultLocked reports whether the vault is locked. It is meaningful only
	// when Mode is "vault".
	VaultLocked bool
	// SpeaksOps reports whether the agent implements [ExtensionOp]. It is false
	// for an agent sshush does not own — a real ssh-agent, 1Password — and for
	// an sshushd older than the extension. Mode is "keys" in both cases, so a
	// caller that needs a vault uses this to tell "wrong backend" from "restart
	// your daemon".
	SpeaksOps bool
}

// Backend reports whether the agent is a vault backend or a plain keyring, and
// for a vault whether it is currently locked, in one round-trip on this
// Session's connection.
//
// A vault answers the vault-locked op. An sshush keyring agent reports that op
// unknown, because it has no vault. Anything else does not implement
// [ExtensionOp] at all and is treated as a keyring with SpeaksOps false.
func (s *Session) Backend() (Backend, error) {
	data, err := s.Op(OpVaultLocked, nil)
	switch {
	case err == nil:
		return Backend{Mode: "vault", VaultLocked: len(data) == 1 && data[0] == 1, SpeaksOps: true}, nil
	case errors.Is(err, ErrOpUnknown):
		return Backend{Mode: "keys", SpeaksOps: true}, nil
	case errors.Is(err, ErrOpUnsupported):
		return Backend{Mode: "keys"}, nil
	default:
		return Backend{}, fmt.Errorf("agent: probe backend: %w", err)
	}
}
