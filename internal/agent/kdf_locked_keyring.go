package agent

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ollykeran/sshush/internal/kdf"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// KDF locked-agent errors mirror golang.org/x/crypto/ssh/agent keyring strings.
var (
	errKDFAgentLocked              = errors.New("agent: locked")
	errKDFAgentNotLocked           = errors.New("agent: not locked")
	errKDFAgentIncorrectPassphrase = errors.New("agent: incorrect passphrase")
)

// KDFLockedKeyring wraps a plain ExtendedAgent and enforces lock/unlock using an Argon2id-derived
// verifier (salt + derived key material in memory only). The inner keyring is never Lock/Unlock'd;
// signing and listing are blocked at this layer when locked.
type KDFLockedKeyring struct {
	mu    sync.Mutex
	inner sshagent.ExtendedAgent

	locked     bool
	salt       []byte
	derivedKey []byte // 32-byte kdf.DeriveKey output
}

// NewKDFLockedKeyring wraps the given agent (typically *sshagent.Keyring) for keys-only mode.
func NewKDFLockedKeyring(inner sshagent.ExtendedAgent) sshagent.ExtendedAgent {
	return &KDFLockedKeyring{inner: inner}
}

func (k *KDFLockedKeyring) wipeVerifier() {
	wipeBytes(k.salt)
	wipeBytes(k.derivedKey)
	k.salt = nil
	k.derivedKey = nil
}

func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// Lock implements sshagent.Agent. Passphrase is used only to derive and store a verifier; it is not retained.
func (k *KDFLockedKeyring) Lock(passphrase []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locked {
		return errKDFAgentLocked
	}
	salt, err := kdf.GenerateSalt()
	if err != nil {
		return fmt.Errorf("agent: generate lock salt: %w", err)
	}
	derived := kdf.DeriveKey(passphrase, salt)
	k.salt = append([]byte(nil), salt...)
	k.derivedKey = derived
	k.locked = true
	return nil
}

// Unlock implements sshagent.Agent.
func (k *KDFLockedKeyring) Unlock(passphrase []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.locked {
		return errKDFAgentNotLocked
	}
	got := kdf.DeriveKey(passphrase, k.salt)
	ok := kdf.ConstantTimeCompare(got, k.derivedKey)
	wipeBytes(got)
	if !ok {
		return errKDFAgentIncorrectPassphrase
	}
	k.wipeVerifier()
	k.locked = false
	return nil
}

// List implements sshagent.Agent. While locked it returns no keys rather than an
// error, matching the x/crypto keyring's behaviour for a locked agent.
func (k *KDFLockedKeyring) List() ([]*sshagent.Key, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locked {
		return nil, nil
	}
	return k.inner.List()
}

// Sign implements sshagent.Agent. It fails while the agent is locked.
func (k *KDFLockedKeyring) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locked {
		return nil, errKDFAgentLocked
	}
	return k.inner.Sign(key, data)
}

// Add implements sshagent.Agent. It fails while the agent is locked.
func (k *KDFLockedKeyring) Add(key sshagent.AddedKey) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locked {
		return errKDFAgentLocked
	}
	return k.inner.Add(key)
}

// Remove implements sshagent.Agent. It fails while the agent is locked.
func (k *KDFLockedKeyring) Remove(key ssh.PublicKey) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locked {
		return errKDFAgentLocked
	}
	return k.inner.Remove(key)
}

// RemoveAll implements sshagent.Agent. It fails while the agent is locked.
func (k *KDFLockedKeyring) RemoveAll() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locked {
		return errKDFAgentLocked
	}
	return k.inner.RemoveAll()
}

// Signers implements sshagent.Agent. It fails while the agent is locked.
func (k *KDFLockedKeyring) Signers() ([]ssh.Signer, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locked {
		return nil, errKDFAgentLocked
	}
	return k.inner.Signers()
}

// SignWithFlags implements sshagent.ExtendedAgent. It fails while the agent is locked.
func (k *KDFLockedKeyring) SignWithFlags(key ssh.PublicKey, data []byte, flags sshagent.SignatureFlags) (*ssh.Signature, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locked {
		return nil, errKDFAgentLocked
	}
	return k.inner.SignWithFlags(key, data, flags)
}

// Extension implements sshagent.ExtendedAgent. It answers [ExtensionOp] itself
// and delegates everything else to the inner agent, which for a plain keyring
// reports every extension unsupported.
func (k *KDFLockedKeyring) Extension(extensionType string, contents []byte) ([]byte, error) {
	if extensionType == ExtensionOp {
		// Never returns an error: a failed op answers with protocol-level success
		// and carries its reason in the body.
		return k.handleOp(contents), nil
	}
	return k.inner.Extension(extensionType, contents)
}

// handleOp serves the lock and unlock operations. A keys-mode agent has no
// vault, so every other op reports itself unsupported.
//
// This is what lets a caller tell "already unlocked" from "wrong passphrase".
// Both reach a plain Unlock as the single string "agent: failure", because
// ServeAgent replaces the keyring's own error with a bare failure byte.
func (k *KDFLockedKeyring) handleOp(contents []byte) []byte {
	op, payload, err := DecodeOpRequest(contents)
	if err != nil {
		return EncodeOpResponse(StatusBadRequest, nil)
	}
	switch op {
	case OpLock:
		return kdfResultFor(k.Lock(payload))
	case OpUnlock:
		return kdfResultFor(k.Unlock(payload))
	default:
		return EncodeOpResponse(StatusUnsupportedOp, nil)
	}
}

// kdfResultFor maps this type's sentinels onto wire statuses.
func kdfResultFor(err error) []byte {
	switch {
	case err == nil:
		return OKResponse()
	case errors.Is(err, errKDFAgentLocked):
		return EncodeOpResponse(StatusLocked, nil)
	case errors.Is(err, errKDFAgentNotLocked):
		return EncodeOpResponse(StatusNotLocked, nil)
	case errors.Is(err, errKDFAgentIncorrectPassphrase):
		return EncodeOpResponse(StatusWrongPassphrase, nil)
	default:
		return EncodeOpResponse(StatusInternal, nil)
	}
}
