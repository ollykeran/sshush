package agent

import (
	"errors"
	"fmt"

	sshagent "golang.org/x/crypto/ssh/agent"
)

// ExtensionOp is the sshush operation extension. It exists because the SSH agent
// protocol destroys the reason an operation failed: ServeAgent answers a failed
// Extension call with a bare SSH_AGENT_EXTENSION_FAILURE byte and discards the
// response body, so the client can only synthesise "agent: generic extension
// failure". Plain operations fare no better — a failed Unlock becomes
// "agent: failure", losing the keyring's own "not locked" and "incorrect
// passphrase".
//
// So a request that fails still answers with protocol-level *success*, and
// carries the reason in the response body instead. Callers get a typed error.
//
// An agent that does not implement this extension reports it unsupported, and
// [Session.Op] surfaces that as [ErrOpUnsupported] so callers can fall back to
// the individual legacy extensions. That keeps a new sshush working against an
// already-running older sshushd, and an older sshush against a newer daemon,
// which still serves the legacy extensions unchanged.
const ExtensionOp = "sshush-op"

// opVersion is the request and response format version. Bump it only for a
// change that an older peer could not parse; adding an op or a status does not
// need a bump, because unknown values have defined meanings on both sides.
const opVersion byte = 1

// Operation codes for [Session.Op]. Values are wire format: never renumber.
const (
	OpVaultLocked    byte = 1 // is this a vault, and is it locked
	OpUnlock         byte = 2 // unlock with a passphrase
	OpLock           byte = 3 // lock
	OpUnlockRecovery byte = 4 // unlock with a BIP-39 recovery phrase
	OpSessionLoad    byte = 5 // make a non-autoload identity visible this session
	OpSessionUnload  byte = 6 // hide an identity for this session
	OpSetAutoload    byte = 7 // persist an identity's autoload flag
	OpSetComment     byte = 8 // persist an identity's comment
	OpAddKey         byte = 9 // add a private key, with autoload
)

// Reasons an operation failed. Callers compare with errors.Is rather than
// matching error text, which is what this extension exists to make possible.
var (
	// ErrOpUnsupported means the agent does not implement [ExtensionOp] at all —
	// an older sshushd, or somebody else's agent. Callers should fall back.
	ErrOpUnsupported = errors.New("agent: sshush-op extension unsupported")
	// ErrOpUnknown means the agent implements the extension but not this op.
	ErrOpUnknown = errors.New("agent: operation not supported by this agent")

	ErrVaultLocked      = errors.New("agent: vault is locked")
	ErrIdentityNotFound = errors.New("agent: identity not found")
	ErrNoRecovery       = errors.New("agent: vault has no recovery phrase")
	ErrWrongPassphrase  = errors.New("agent: incorrect passphrase")
	ErrNotLocked        = errors.New("agent: not locked")
	ErrBadRequest       = errors.New("agent: malformed request")
	ErrAgentInternal    = errors.New("agent: operation failed")
)

// EncodeOpRequest builds an op request body: version, op code, then payload.
func EncodeOpRequest(op byte, payload []byte) []byte {
	req := make([]byte, 2+len(payload))
	req[0] = opVersion
	req[1] = op
	copy(req[2:], payload)
	return req
}

// DecodeOpRequest parses an op request body. Agent implementations call this
// from their Extension method when the type is [ExtensionOp].
func DecodeOpRequest(contents []byte) (op byte, payload []byte, err error) {
	if len(contents) < 2 {
		return 0, nil, fmt.Errorf("agent: op request too short (%d bytes)", len(contents))
	}
	if contents[0] != opVersion {
		return 0, nil, fmt.Errorf("agent: unsupported op request version %d", contents[0])
	}
	return contents[1], contents[2:], nil
}

// EncodeOpResponse builds an op response body. The response is never empty,
// because ServeAgent treats an empty extension response as no reply at all.
func EncodeOpResponse(status byte, data []byte) []byte {
	resp := make([]byte, 2+len(data))
	resp[0] = opVersion
	resp[1] = status
	copy(resp[2:], data)
	return resp
}

// OKResponse is the success response carrying no data, which is the common case.
func OKResponse() []byte { return EncodeOpResponse(StatusOK, nil) }

// Status codes for agent implementations mapping their own errors onto the wire.
// Values are wire format: never renumber.
const (
	StatusOK              byte = 0
	StatusLocked          byte = 1
	StatusNotFound        byte = 2
	StatusNoRecovery      byte = 3
	StatusBadRequest      byte = 4
	StatusUnsupportedOp   byte = 5
	StatusWrongPassphrase byte = 6
	StatusNotLocked       byte = 7
	StatusInternal        byte = 8
)

// DecodeOpResponse parses an op response body, returning its data or the
// sentinel naming why the operation failed. It is the mirror of
// [EncodeOpResponse], and is what [Session.Op] uses once the reply arrives.
func DecodeOpResponse(resp []byte) ([]byte, error) {
	if len(resp) < 2 {
		return nil, fmt.Errorf("agent: op: short response (%d bytes)", len(resp))
	}
	if resp[0] != opVersion {
		return nil, fmt.Errorf("agent: op: unsupported response version %d", resp[0])
	}
	if err := errorForStatus(resp[1]); err != nil {
		return nil, err
	}
	return resp[2:], nil
}

// errorForStatus maps a response status to the sentinel callers match on.
func errorForStatus(status byte) error {
	switch status {
	case StatusOK:
		return nil
	case StatusLocked:
		return ErrVaultLocked
	case StatusNotFound:
		return ErrIdentityNotFound
	case StatusNoRecovery:
		return ErrNoRecovery
	case StatusBadRequest:
		return ErrBadRequest
	case StatusUnsupportedOp:
		return ErrOpUnknown
	case StatusWrongPassphrase:
		return ErrWrongPassphrase
	case StatusNotLocked:
		return ErrNotLocked
	default:
		return ErrAgentInternal
	}
}

// Op performs an operation over this Session and returns the response data, or
// a typed error naming the reason it failed. An agent that does not implement
// [ExtensionOp] yields [ErrOpUnsupported]; callers fall back to the legacy
// per-operation extensions in that case.
func (s *Session) Op(op byte, payload []byte) ([]byte, error) {
	resp, err := s.client.Extension(ExtensionOp, EncodeOpRequest(op, payload))
	if err != nil {
		if errors.Is(err, sshagent.ErrExtensionUnsupported) {
			return nil, ErrOpUnsupported
		}
		// A protocol-level extension failure means the agent implements the
		// extension but could not answer at all, so there is no reason byte.
		return nil, fmt.Errorf("agent: op %d: %w", op, err)
	}
	return DecodeOpResponse(resp)
}
