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
// Every operation travels through the sshush-op extension, so a failure names
// its reason. Match reasons with errors.Is against the agent.Err* sentinels.
// An agent that does not implement the extension — a foreign agent, or an
// sshushd older than it — reports [agent.ErrOpUnsupported].

// AddPrivateKeyFile adds the private key at path to the vault behind s, with
// autoload controlling whether the identity is loaded again after the daemon
// restarts. The caller is responsible for having established that s is a vault
// backend; a keyring agent reports the operation unknown.
//
// The error is wrapped, so match the reason with errors.Is rather than on text.
func AddPrivateKeyFile(s *agent.Session, path string, autoload bool) error {
	payload, err := BuildAddKeyOptsPayload(path, autoload)
	if err != nil {
		return fmt.Errorf("vault: build add-key-opts payload: %w", err)
	}
	if _, err := s.Op(agent.OpAddKey, payload); err != nil {
		return fmt.Errorf("vault: add key to vault: %w", err)
	}
	return nil
}

// SetAutoload persists whether the identity with the given fingerprint is
// loaded automatically when the vault is unlocked.
func SetAutoload(s *agent.Session, fingerprint string, on bool) error {
	_, err := s.Op(agent.OpSetAutoload, BuildSetAutoloadPayload(fingerprint, on))
	return err
}

// SetComment persists a new comment for the identity with the given fingerprint.
func SetComment(s *agent.Session, fingerprint, comment string) error {
	_, err := s.Op(agent.OpSetComment, BuildSetCommentPayload(fingerprint, comment))
	return err
}

// SessionLoad makes a non-autoload identity visible in the running agent until
// that agent restarts.
func SessionLoad(s *agent.Session, fingerprint string) error {
	_, err := s.Op(agent.OpSessionLoad, []byte(fingerprint))
	return err
}

// SessionUnload hides an identity from the running agent for this session only,
// leaving the autoload flag stored on disk untouched.
func SessionUnload(s *agent.Session, fingerprint string) error {
	_, err := s.Op(agent.OpSessionUnload, []byte(fingerprint))
	return err
}

// UnlockWithRecoveryPhrase unlocks the vault behind s with its 24-word BIP-39
// recovery phrase. A vault created with --no-recovery reports
// [agent.ErrNoRecovery] rather than a wrong-phrase error.
func UnlockWithRecoveryPhrase(s *agent.Session, mnemonic string) error {
	_, err := s.Op(agent.OpUnlockRecovery, []byte(mnemonic))
	return err
}
