package agent

import (
	"errors"
	"testing"

	sshagent "golang.org/x/crypto/ssh/agent"
)

// opAgent answers ExtensionOp from a table of op -> response, and reports every
// other extension unsupported. It stands in for a vault agent without importing
// internal/vault, which imports this package.
type opAgent struct {
	sshagent.ExtendedAgent
	responses map[byte][]byte
	seen      []byte // ops received, in order
}

func (o *opAgent) Extension(extensionType string, contents []byte) ([]byte, error) {
	if extensionType != ExtensionOp {
		return nil, sshagent.ErrExtensionUnsupported
	}
	op, _, err := DecodeOpRequest(contents)
	if err != nil {
		return EncodeOpResponse(StatusBadRequest, nil), nil
	}
	o.seen = append(o.seen, op)
	if resp, ok := o.responses[op]; ok {
		return resp, nil
	}
	return EncodeOpResponse(StatusUnsupportedOp, nil), nil
}

// legacyAgent implements no extensions at all, standing in for an older sshushd
// or somebody else's agent.
type legacyAgent struct{ sshagent.ExtendedAgent }

func (legacyAgent) Extension(string, []byte) ([]byte, error) {
	// Must be the bare sentinel: ServeAgent compares it with ==, not errors.Is.
	return nil, sshagent.ErrExtensionUnsupported
}

func TestEncodeDecodeOpRequestRoundTrip(t *testing.T) {
	op, payload, err := DecodeOpRequest(EncodeOpRequest(OpSetAutoload, []byte("body")))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if op != OpSetAutoload {
		t.Errorf("op: want %d, got %d", OpSetAutoload, op)
	}
	if string(payload) != "body" {
		t.Errorf("payload: want %q, got %q", "body", string(payload))
	}
}

func TestDecodeOpRequest_rejectsShortAndWrongVersion(t *testing.T) {
	if _, _, err := DecodeOpRequest([]byte{opVersion}); err == nil {
		t.Error("want error for a one-byte request, got nil")
	}
	if _, _, err := DecodeOpRequest([]byte{opVersion + 9, OpLock}); err == nil {
		t.Error("want error for an unknown request version, got nil")
	}
}

func TestOpResponseIsNeverEmpty(t *testing.T) {
	// ServeAgent turns an empty extension response into no reply at all, so the
	// success case must still carry its two header bytes.
	if got := len(OKResponse()); got < 2 {
		t.Errorf("OKResponse length: want at least 2, got %d", got)
	}
}

func TestSessionOp_success(t *testing.T) {
	ext := &opAgent{
		ExtendedAgent: keyringWithKey(t, "op-test"),
		responses:     map[byte][]byte{OpVaultLocked: EncodeOpResponse(StatusOK, []byte{1})},
	}
	s := openSession(t, startServerAgent(t, ext))

	data, err := s.Op(OpVaultLocked, nil)
	if err != nil {
		t.Fatalf("op: %v", err)
	}
	if len(data) != 1 || data[0] != 1 {
		t.Errorf("data: want [1], got %v", data)
	}
}

func TestSessionOp_typedFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status byte
		want   error
	}{
		{"locked", StatusLocked, ErrVaultLocked},
		{"not found", StatusNotFound, ErrIdentityNotFound},
		{"no recovery", StatusNoRecovery, ErrNoRecovery},
		{"bad request", StatusBadRequest, ErrBadRequest},
		{"wrong passphrase", StatusWrongPassphrase, ErrWrongPassphrase},
		{"not locked", StatusNotLocked, ErrNotLocked},
		{"internal", StatusInternal, ErrAgentInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ext := &opAgent{
				ExtendedAgent: keyringWithKey(t, "op-test"),
				responses:     map[byte][]byte{OpSessionLoad: EncodeOpResponse(tc.status, nil)},
			}
			s := openSession(t, startServerAgent(t, ext))

			_, err := s.Op(OpSessionLoad, nil)
			if !errors.Is(err, tc.want) {
				t.Errorf("error: want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestSessionOp_unknownOpIsDistinctFromUnsupportedExtension(t *testing.T) {
	// The agent speaks the extension but not this op: the caller must not treat
	// that as "fall back to the legacy extensions".
	ext := &opAgent{ExtendedAgent: keyringWithKey(t, "op-test"), responses: map[byte][]byte{}}
	s := openSession(t, startServerAgent(t, ext))

	_, err := s.Op(OpSetComment, nil)
	if !errors.Is(err, ErrOpUnknown) {
		t.Fatalf("error: want ErrOpUnknown, got %v", err)
	}
	if errors.Is(err, ErrOpUnsupported) {
		t.Error("ErrOpUnknown must not also match ErrOpUnsupported")
	}
}

func TestSessionOp_olderAgentReportsUnsupported(t *testing.T) {
	// An older sshushd, or a non-sshush agent, knows nothing of sshush-op. The
	// caller needs ErrOpUnsupported so it can fall back to the legacy extensions.
	s := openSession(t, startServerAgent(t, legacyAgent{keyringWithKey(t, "legacy")}))

	_, err := s.Op(OpVaultLocked, nil)
	if !errors.Is(err, ErrOpUnsupported) {
		t.Fatalf("error: want ErrOpUnsupported, got %v", err)
	}
}

func TestSessionOp_payloadReachesTheAgent(t *testing.T) {
	ext := &opAgent{
		ExtendedAgent: keyringWithKey(t, "op-test"),
		responses:     map[byte][]byte{OpSetAutoload: OKResponse()},
	}
	s := openSession(t, startServerAgent(t, ext))

	if _, err := s.Op(OpSetAutoload, []byte("payload")); err != nil {
		t.Fatalf("op: %v", err)
	}
	if len(ext.seen) != 1 || ext.seen[0] != OpSetAutoload {
		t.Errorf("ops seen: want [%d], got %v", OpSetAutoload, ext.seen)
	}
}

// TestKDFKeyringOp_unlockReasonsSurvive covers the defect this extension exists
// to fix. Over a plain Unlock, "already unlocked" and "wrong passphrase" both
// reach the caller as the single string "agent: failure", because ServeAgent
// replaces the keyring's own error with a bare failure byte.
func TestKDFKeyringOp_unlockReasonsSurvive(t *testing.T) {
	passphrase := []byte("kdf-op-test")
	ext := NewKDFLockedKeyring(keyringWithKey(t, "kdf-op"))
	s := openSession(t, startServerAgent(t, ext))

	// Unlocking an agent that was never locked.
	if _, err := s.Op(OpUnlock, passphrase); !errors.Is(err, ErrNotLocked) {
		t.Errorf("unlock while unlocked: want ErrNotLocked, got %v", err)
	}
	// The same call through the plain protocol loses the reason entirely.
	if err := s.Unlock(passphrase); err == nil || err.Error() != "agent: failure" {
		t.Errorf("plain unlock: want the opaque %q, got %v", "agent: failure", err)
	}

	if _, err := s.Op(OpLock, passphrase); err != nil {
		t.Fatalf("lock op: %v", err)
	}
	if _, err := s.Op(OpUnlock, []byte("wrong")); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("wrong passphrase: want ErrWrongPassphrase, got %v", err)
	}
	if _, err := s.Op(OpUnlock, passphrase); err != nil {
		t.Errorf("unlock with the right passphrase: %v", err)
	}
}

func TestKDFKeyringOp_lockTwiceReportsLocked(t *testing.T) {
	ext := NewKDFLockedKeyring(keyringWithKey(t, "kdf-op"))
	s := openSession(t, startServerAgent(t, ext))

	if _, err := s.Op(OpLock, []byte("pw")); err != nil {
		t.Fatalf("first lock: %v", err)
	}
	if _, err := s.Op(OpLock, []byte("pw")); !errors.Is(err, ErrVaultLocked) {
		t.Errorf("second lock: want ErrVaultLocked, got %v", err)
	}
}

func TestKDFKeyringOp_vaultOpsAreUnsupported(t *testing.T) {
	// A keys-mode agent has no vault, so vault ops must report themselves
	// unsupported rather than answering misleadingly.
	ext := NewKDFLockedKeyring(keyringWithKey(t, "kdf-op"))
	s := openSession(t, startServerAgent(t, ext))

	if _, err := s.Op(OpSessionLoad, []byte("SHA256:x")); !errors.Is(err, ErrOpUnknown) {
		t.Errorf("session-load on a keyring: want ErrOpUnknown, got %v", err)
	}
}

// TestKDFKeyringOp_backendStillReportsKeys guards the Session.Backend contract:
// serving sshush-op must not make a keyring look like a vault, because Backend
// probes the legacy vault-locked extension.
func TestKDFKeyringOp_backendStillReportsKeys(t *testing.T) {
	ext := NewKDFLockedKeyring(keyringWithKey(t, "kdf-op"))
	s := openSession(t, startServerAgent(t, ext))

	backend, err := s.Backend()
	if err != nil {
		t.Fatalf("backend: %v", err)
	}
	if backend.Mode != "keys" {
		t.Errorf("mode: want %q, got %q", "keys", backend.Mode)
	}
}
