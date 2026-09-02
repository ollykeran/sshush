package vault

import (
	"errors"
	"fmt"

	"github.com/ollykeran/sshush/internal/agent"
)

// This file is the vault's half of the agent seam: package agent owns the
// connection, package vault owns the extension vocabulary. The split exists
// because package agent cannot import this one — the dependency runs the other
// way.
//
// Each function prefers the sshush-op extension, where a failure names its
// reason, and falls back to the legacy per-operation extension against an agent
// that does not implement it. Match reasons with errors.Is against the
// agent.Err* sentinels; against an older daemon no reason is available and the
// error is the opaque protocol string, as it always was.

// callOp performs op over s, falling back to the legacy extension when the agent
// does not implement [agent.ExtensionOp] — an older sshushd, or somebody else's.
// On the op path the returned error names the reason and is matched with
// errors.Is; on the fallback path it is the opaque protocol error, exactly as
// before this extension existed.
func callOp(s *agent.Session, op byte, legacyExtension string, payload []byte) error {
	_, err := s.Op(op, payload)
	if !errors.Is(err, agent.ErrOpUnsupported) {
		return err
	}
	_, err = s.Extension(legacyExtension, payload)
	return err
}

// AddPrivateKeyFile adds the private key at path to the vault behind s, with
// autoload controlling whether the identity is loaded again after the daemon
// restarts. The caller is responsible for having established that s is a vault
// backend; a plain keyring reports the operation unsupported.
//
// The error is wrapped, so match the reason with errors.Is rather than on text.
func AddPrivateKeyFile(s *agent.Session, path string, autoload bool) error {
	payload, err := BuildAddKeyOptsPayload(path, autoload)
	if err != nil {
		return fmt.Errorf("vault: build add-key-opts payload: %w", err)
	}
	if err := callOp(s, agent.OpAddKey, ExtensionAddKeyOpts, payload); err != nil {
		return fmt.Errorf("vault: call add-key-opts extension: %w", err)
	}
	return nil
}

// SetAutoload persists whether the identity with the given fingerprint is
// loaded automatically when the vault is unlocked.
func SetAutoload(s *agent.Session, fingerprint string, on bool) error {
	return callOp(s, agent.OpSetAutoload, ExtensionVaultSetAutoload, BuildSetAutoloadPayload(fingerprint, on))
}

// SetComment persists a new comment for the identity with the given fingerprint.
func SetComment(s *agent.Session, fingerprint, comment string) error {
	return callOp(s, agent.OpSetComment, ExtensionVaultSetComment, BuildSetCommentPayload(fingerprint, comment))
}

// SessionLoad makes a non-autoload identity visible in the running agent until
// that agent restarts.
func SessionLoad(s *agent.Session, fingerprint string) error {
	return callOp(s, agent.OpSessionLoad, ExtensionVaultSessionLoad, []byte(fingerprint))
}

// SessionUnload hides an identity from the running agent for this session only,
// leaving the autoload flag stored on disk untouched.
func SessionUnload(s *agent.Session, fingerprint string) error {
	return callOp(s, agent.OpSessionUnload, ExtensionVaultSessionUnload, []byte(fingerprint))
}

// UnlockWithRecoveryPhrase unlocks the vault behind s with its 24-word BIP-39
// recovery phrase. A vault created with --no-recovery reports
// [agent.ErrNoRecovery] rather than a wrong-phrase error.
func UnlockWithRecoveryPhrase(s *agent.Session, mnemonic string) error {
	return callOp(s, agent.OpUnlockRecovery, ExtensionUnlockRecovery, []byte(mnemonic))
}
