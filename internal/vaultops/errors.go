package vaultops

import (
	"errors"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/openssh"
	"github.com/ollykeran/sshush/internal/vault"
)

// Code classifies why a vault operation failed, so a front end can branch on
// the reason without matching text.
type Code int

// The reasons a vault operation can fail.
const (
	// CodeUnknown is the zero value; no verb returns it.
	CodeUnknown Code = iota
	// CodeNoVaultPath means no vault path was configured or passed.
	CodeNoVaultPath
	// CodeVaultNotInitialized means the vault path names no initialized vault.
	CodeVaultNotInitialized
	// CodeVaultExists means init was asked to create a vault that is there.
	CodeVaultExists
	// CodeVaultUnreadable means the vault file is present but unusable.
	CodeVaultUnreadable
	// CodeNoAgent means nothing answered on the agent socket.
	CodeNoAgent
	// CodeNotVaultAgent means an agent answered but is not vault-backed.
	CodeNotVaultAgent
	// CodeVaultLocked means the agent holds no master key right now.
	CodeVaultLocked
	// CodeIdentityNotFound means no identity matched the selector.
	CodeIdentityNotFound
	// CodeAmbiguousSelector means more than one identity matched the selector.
	CodeAmbiguousSelector
	// CodeEncryptedKey means the key file is passphrase-protected.
	CodeEncryptedKey
	// CodeNoRecovery means the vault was created without a recovery phrase.
	CodeNoRecovery
	// CodeWrongPassphrase means the passphrase or recovery phrase was wrong.
	CodeWrongPassphrase
	// CodeNotLocked means an unlock was asked of an already-unlocked vault.
	CodeNotLocked
	// CodeAgentFailed means the agent refused for a reason it did not name.
	CodeAgentFailed
	// CodeLocalIO means reading or writing a local file failed.
	CodeLocalIO
)

// OpError is a vault operation failure phrased once, for both front ends. Msg
// is a single sentence naming what went wrong. Hint, when set, is the remedy,
// kept out of Msg so the CLI can render it as its own line and the TUI can drop
// it when the status line has no room. Err is the underlying cause, so
// errors.Is against the agent and vault sentinels still works.
type OpError struct {
	Code Code
	Msg  string
	Hint string
	Err  error
}

// Error returns Msg alone; Hint is for the front end to render.
func (e *OpError) Error() string { return e.Msg }

// Unwrap returns the underlying cause, or nil.
func (e *OpError) Unwrap() error { return e.Err }

// HintOf returns err's remedy sentence when err is an [*OpError], else "".
func HintOf(err error) string {
	var op *OpError
	if errors.As(err, &op) {
		return op.Hint
	}
	return ""
}

// CodeOf returns err's [Code] when err is an [*OpError], else [CodeUnknown].
func CodeOf(err error) Code {
	var op *OpError
	if errors.As(err, &op) {
		return op.Code
	}
	return CodeUnknown
}

// describe turns an error from the agent or the vault store into the OpError
// both front ends render. verb names the operation ("remove", "load") and
// selector, when non-empty, is the identity spec the user typed, so
// "no vault identity matches <selector>" reads the same everywhere.
func describe(err error, verb, selector string) error {
	if err == nil {
		return nil
	}
	var already *OpError
	if errors.As(err, &already) {
		return already
	}
	switch {
	case errors.Is(err, agent.ErrVaultLocked):
		return &OpError{
			Code: CodeVaultLocked,
			Msg:  "vault is locked",
			Hint: "Unlock it with 'sshush unlock' or 'sshush vault unlock-recovery'.",
			Err:  err,
		}
	case errors.Is(err, agent.ErrIdentityNotFound), errors.Is(err, vault.ErrIdentityNotFound):
		msg := "identity not found in vault"
		if selector != "" {
			msg = "no vault identity matches " + selector
		}
		return &OpError{Code: CodeIdentityNotFound, Msg: msg, Err: err}
	case errors.Is(err, vault.ErrAmbiguousComment):
		return &OpError{
			Code: CodeAmbiguousSelector,
			Msg:  "ambiguous comment: multiple vault identities share that comment",
			Hint: "Use the fingerprint instead.",
			Err:  err,
		}
	case errors.Is(err, openssh.ErrEncryptedPrivateKey):
		return &OpError{Code: CodeEncryptedKey, Msg: err.Error(), Err: err}
	case errors.Is(err, agent.ErrNoRecovery):
		return &OpError{
			Code: CodeNoRecovery,
			Msg:  "this vault was created without a recovery phrase, so no phrase can unlock it",
			Err:  err,
		}
	case errors.Is(err, agent.ErrWrongPassphrase):
		if verb == "unlock-recovery" {
			return &OpError{
				Code: CodeWrongPassphrase,
				Msg:  "unlock failed: wrong recovery phrase",
				Hint: "Use exactly 24 words, single spaces.",
				Err:  err,
			}
		}
		return &OpError{Code: CodeWrongPassphrase, Msg: "unlock failed: wrong passphrase", Err: err}
	case errors.Is(err, agent.ErrNotLocked):
		return &OpError{Code: CodeNotLocked, Msg: "vault is already unlocked", Err: err}
	case errors.Is(err, agent.ErrOpUnsupported):
		// The agent does not implement sshush-op at all.
		return gateError(verb, agent.Backend{}, err)
	case errors.Is(err, agent.ErrOpUnknown):
		// It speaks the protocol but not this operation, so it is a keyring.
		return gateError(verb, agent.Backend{SpeaksOps: true}, err)
	default:
		return &OpError{Code: CodeAgentFailed, Msg: verb + " failed: " + err.Error(), Err: err}
	}
}
