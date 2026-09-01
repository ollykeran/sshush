package vault

import (
	"fmt"

	"github.com/ollykeran/sshush/internal/agent"
)

// This file is the vault's half of the agent seam: package agent owns the
// connection, package vault owns the extension vocabulary. The split exists
// because package agent cannot import this one — the dependency runs the other
// way.
//
// Except where noted, these functions return the extension error unmodified.
// Callers tell a locked vault from a real failure by matching the exact text
// golang.org/x/crypto/ssh/agent produces ("agent: generic extension failure"),
// so wrapping here would silently change user-facing messages.

// AddPrivateKeyFile adds the private key at path to the vault behind s, with
// autoload controlling whether the identity is loaded again after the daemon
// restarts. The caller is responsible for having established that s is a vault
// backend; a plain keyring reports the add-key-opts extension unsupported.
//
// Unlike the others this wraps the extension error, preserving the message
// callers have seen since this function dialled its own socket.
func AddPrivateKeyFile(s *agent.Session, path string, autoload bool) error {
	payload, err := BuildAddKeyOptsPayload(path, autoload)
	if err != nil {
		return fmt.Errorf("vault: build add-key-opts payload: %w", err)
	}
	if _, err := s.Extension(ExtensionAddKeyOpts, payload); err != nil {
		return fmt.Errorf("vault: call add-key-opts extension: %w", err)
	}
	return nil
}

// SetAutoload persists whether the identity with the given fingerprint is
// loaded automatically when the vault is unlocked.
func SetAutoload(s *agent.Session, fingerprint string, on bool) error {
	_, err := s.Extension(ExtensionVaultSetAutoload, BuildSetAutoloadPayload(fingerprint, on))
	return err
}

// SetComment persists a new comment for the identity with the given fingerprint.
func SetComment(s *agent.Session, fingerprint, comment string) error {
	_, err := s.Extension(ExtensionVaultSetComment, BuildSetCommentPayload(fingerprint, comment))
	return err
}

// SessionLoad makes a non-autoload identity visible in the running agent until
// that agent restarts.
func SessionLoad(s *agent.Session, fingerprint string) error {
	_, err := s.Extension(ExtensionVaultSessionLoad, []byte(fingerprint))
	return err
}

// SessionUnload hides an identity from the running agent for this session only,
// leaving the autoload flag stored on disk untouched.
func SessionUnload(s *agent.Session, fingerprint string) error {
	_, err := s.Extension(ExtensionVaultSessionUnload, []byte(fingerprint))
	return err
}

// UnlockWithRecoveryPhrase unlocks the vault behind s with its 24-word BIP-39
// recovery phrase.
func UnlockWithRecoveryPhrase(s *agent.Session, mnemonic string) error {
	_, err := s.Extension(ExtensionUnlockRecovery, []byte(mnemonic))
	return err
}
