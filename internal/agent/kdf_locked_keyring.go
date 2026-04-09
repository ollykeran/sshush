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
	errKDFAgentLocked            = errors.New("agent: locked")
	errKDFAgentNotLocked         = errors.New("agent: not locked")
	errKDFAgentIncorrectPassphrase = fmt.Errorf("agent: incorrect passphrase")
)

// KDFLockedKeyring wraps a plain ExtendedAgent and enforces lock/unlock using an Argon2id-derived
// verifier (salt + derived key material in memory only). The inner keyring is never Lock/Unlock'd;
// signing and listing are blocked at this layer when locked.
type KDFLockedKeyring struct {
	mu sync.Mutex
	inner sshagent.ExtendedAgent

	locked       bool
	salt         []byte
	derivedKey   []byte // 32-byte kdf.DeriveKey output
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
		return err
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

func (k *KDFLockedKeyring) List() ([]*sshagent.Key, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locked {
		return nil, nil
	}
	return k.inner.List()
}

func (k *KDFLockedKeyring) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locked {
		return nil, errKDFAgentLocked
	}
	return k.inner.Sign(key, data)
}

func (k *KDFLockedKeyring) Add(key sshagent.AddedKey) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locked {
		return errKDFAgentLocked
	}
	return k.inner.Add(key)
}

func (k *KDFLockedKeyring) Remove(key ssh.PublicKey) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locked {
		return errKDFAgentLocked
	}
	return k.inner.Remove(key)
}

func (k *KDFLockedKeyring) RemoveAll() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locked {
		return errKDFAgentLocked
	}
	return k.inner.RemoveAll()
}

func (k *KDFLockedKeyring) Signers() ([]ssh.Signer, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locked {
		return nil, errKDFAgentLocked
	}
	return k.inner.Signers()
}

func (k *KDFLockedKeyring) SignWithFlags(key ssh.PublicKey, data []byte, flags sshagent.SignatureFlags) (*ssh.Signature, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locked {
		return nil, errKDFAgentLocked
	}
	return k.inner.SignWithFlags(key, data, flags)
}

func (k *KDFLockedKeyring) Extension(extensionType string, contents []byte) ([]byte, error) {
	return k.inner.Extension(extensionType, contents)
}
