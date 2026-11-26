package vault

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ollykeran/sshush/internal/kdf"
	"github.com/tyler-smith/go-bip39"
	ssh "golang.org/x/crypto/ssh"
)

// The master key is derived from the passphrase (KDF(passphrase, salt)), so a
// new passphrase means a new master key, and every blob encrypted under the
// old one must be encrypted again: each identity and the canary. The recovery
// phrase wraps the master key, so it stops working too and is issued afresh.

// ErrWrongPassphrase and ErrNoRecoveryEnabled are what working on a vault
// file directly reports for a credential that does not open it, and for a
// recovery phrase offered to a vault without one.
var (
	ErrWrongPassphrase   = errWrongPassphrase
	ErrNoRecoveryEnabled = errNoRecovery
)

// ErrWeakPassphrase means a new passphrase does not meet DefaultPassphrasePolicy.
var ErrWeakPassphrase = errors.New("vault: new passphrase rejected")

// ErrBadMnemonic means a recovery phrase is not a valid 24-word BIP-39 phrase.
var ErrBadMnemonic = errors.New("vault: recovery phrase is not a valid BIP-39 phrase")

// Credential proves the right to change a vault: its passphrase, or its
// recovery phrase when Mnemonic is set. It is checked against the vault file
// itself, never taken from an agent being unlocked, so whoever can reach an
// unlocked agent still cannot change its passphrase without knowing a secret.
type Credential struct {
	Passphrase []byte
	Mnemonic   string
}

// masterKeyFrom derives the vault's master key from cred and proves it against
// the canary. The caller owns the returned key and must wipe it.
func masterKeyFrom(meta *VaultMetadata, cred Credential) ([]byte, error) {
	if cred.Mnemonic == "" {
		return masterKeyFromPassphrase(meta, cred.Passphrase)
	}
	if meta == nil || len(meta.RecoverySalt) == 0 || len(meta.WrappedMasterKey) == 0 {
		return nil, errNoRecovery
	}
	recoveryKey := kdf.DeriveKey([]byte(normalizeMnemonic(cred.Mnemonic)), meta.RecoverySalt)
	defer wipe(recoveryKey)
	masterKey, err := decryptBlob(recoveryKey, meta.WrappedMasterKey)
	if err != nil {
		return nil, errWrongPassphrase
	}
	canary, err := decryptBlob(masterKey, meta.Canary)
	if err != nil || !kdf.ConstantTimeCompare(canary, []byte(canaryPlaintext)) {
		wipe(masterKey)
		return nil, errWrongPassphrase
	}
	wipe(canary)
	return masterKey, nil
}

// normalizeMnemonic collapses the whitespace in a typed phrase, as unlocking
// with one does.
func normalizeMnemonic(m string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(m)), " ")
}

// checkMnemonic accepts "" (no recovery) or a valid BIP-39 phrase.
func checkMnemonic(m string) error {
	if m != "" && !bip39.IsMnemonicValid(normalizeMnemonic(m)) {
		return ErrBadMnemonic
	}
	return nil
}

// ChangePassphrase proves cred against the vault in store, then re-encrypts
// the vault under newPass: a new salt and master key, every identity and the
// canary encrypted again. newMnemonic becomes the recovery phrase; "" leaves
// the vault without one. The vault is saved once, at the end, so a failure
// anywhere leaves it as it was, in memory and on disk.
//
// It returns the new master key, which the caller owns and must wipe; an
// agent that was unlocked keeps serving with it.
func ChangePassphrase(store *VaultStore, cred Credential, newPass []byte, newMnemonic string) ([]byte, error) {
	if err := DefaultPassphrasePolicy.ValidateNew(newPass); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrWeakPassphrase, err)
	}
	if err := checkMnemonic(newMnemonic); err != nil {
		return nil, err
	}
	oldMaster, err := masterKeyFrom(store.GetMetadata(), cred)
	if err != nil {
		return nil, err
	}
	defer wipe(oldMaster)
	return rekey(store, oldMaster, newPass, normalizeMnemonic(newMnemonic))
}

// rekey re-encrypts store from oldMaster to a key derived from newPass.
func rekey(store *VaultStore, oldMaster, newPass []byte, newMnemonic string) ([]byte, error) {
	meta := store.GetMetadata()
	if meta == nil {
		return nil, errWrongPassphrase
	}
	salt, err := kdf.GenerateSalt()
	if err != nil {
		return nil, fmt.Errorf("vault: generate salt: %w", err)
	}
	newMaster := kdf.DeriveKey(newPass, salt)
	ok := false
	defer func() {
		if !ok {
			wipe(newMaster)
		}
	}()

	ids := store.AllIdentities()
	for i := range ids {
		plain, err := decryptBlob(oldMaster, ids[i].EncryptedBlob)
		if err != nil {
			return nil, fmt.Errorf("vault: decrypt %s: %w", ids[i].Fingerprint, err)
		}
		blob, err := encryptBlob(newMaster, plain)
		wipe(plain)
		if err != nil {
			return nil, fmt.Errorf("vault: encrypt %s: %w", ids[i].Fingerprint, err)
		}
		ids[i].EncryptedBlob = blob
	}
	canary, err := encryptBlob(newMaster, []byte(canaryPlaintext))
	if err != nil {
		return nil, fmt.Errorf("vault: encrypt canary: %w", err)
	}
	next := &VaultMetadata{Salt: salt, Canary: canary, CreatedAt: meta.CreatedAt}
	if newMnemonic != "" {
		if next.RecoverySalt, next.WrappedMasterKey, err = wrapMasterKey(newMaster, newMnemonic); err != nil {
			return nil, err
		}
	}
	if err := store.replace(next, ids); err != nil {
		return nil, err
	}
	ok = true
	return newMaster, nil
}

// wrapMasterKey encrypts masterKey under a key derived from mnemonic, for
// recovery.
func wrapMasterKey(masterKey []byte, mnemonic string) (salt, wrapped []byte, err error) {
	salt, err = kdf.GenerateSalt()
	if err != nil {
		return nil, nil, fmt.Errorf("vault: generate recovery salt: %w", err)
	}
	recoveryKey := kdf.DeriveKey([]byte(mnemonic), salt)
	defer wipe(recoveryKey)
	wrapped, err = encryptBlob(recoveryKey, masterKey)
	if err != nil {
		return nil, nil, fmt.Errorf("vault: encrypt master key with recovery key: %w", err)
	}
	return salt, wrapped, nil
}

// SetRecovery proves cred against the vault in store and makes mnemonic its
// recovery phrase, replacing any it had: the old phrase stops working. The
// passphrase, and so every key's encryption, is unchanged.
func SetRecovery(store *VaultStore, cred Credential, mnemonic string) error {
	if mnemonic == "" {
		return ErrBadMnemonic
	}
	if err := checkMnemonic(mnemonic); err != nil {
		return err
	}
	masterKey, err := masterKeyFrom(store.GetMetadata(), cred)
	if err != nil {
		return err
	}
	defer wipe(masterKey)
	salt, wrapped, err := wrapMasterKey(masterKey, normalizeMnemonic(mnemonic))
	if err != nil {
		return err
	}
	meta := store.GetMetadata()
	meta.RecoverySalt, meta.WrappedMasterKey = salt, wrapped
	return store.replace(meta, nil)
}

// changePassphrase is ChangePassphrase for the running agent: the vault stays
// as locked or unlocked as it was, and if unlocked, under its new master key.
func (a *VaultAgent) changePassphrase(cred Credential, newPass []byte, newMnemonic string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	newMaster, err := ChangePassphrase(a.store, cred, newPass, newMnemonic)
	if err != nil {
		return err
	}
	if a.masterKey == nil {
		wipe(newMaster)
		return nil
	}
	wipe(a.masterKey)
	a.masterKey = newMaster
	return nil
}

// setRecovery is SetRecovery for the running agent.
func (a *VaultAgent) setRecovery(cred Credential, mnemonic string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return SetRecovery(a.store, cred, mnemonic)
}

// Wire payloads for OpChangePassphrase and OpSetRecovery, in SSH wire format.
// CredKind is 0 for a passphrase, 1 for a recovery phrase.
type changePassphraseWire struct {
	CredKind    uint8
	Credential  []byte
	NewPass     []byte
	NewMnemonic string
}

type setRecoveryWire struct {
	CredKind   uint8
	Credential []byte
	Mnemonic   string
}

func credentialWire(c Credential) (uint8, []byte) {
	if c.Mnemonic != "" {
		return 1, []byte(c.Mnemonic)
	}
	return 0, c.Passphrase
}

func credentialFromWire(kind uint8, b []byte) (Credential, error) {
	switch kind {
	case 0:
		return Credential{Passphrase: b}, nil
	case 1:
		return Credential{Mnemonic: string(b)}, nil
	}
	return Credential{}, fmt.Errorf("vault: unknown credential kind %d", kind)
}

// BuildChangePassphrasePayload is the OpChangePassphrase request body. The
// caller should clear it after sending: it holds both passphrases.
func BuildChangePassphrasePayload(cred Credential, newPass []byte, newMnemonic string) []byte {
	kind, b := credentialWire(cred)
	return ssh.Marshal(changePassphraseWire{CredKind: kind, Credential: b, NewPass: newPass, NewMnemonic: newMnemonic})
}

func parseChangePassphrasePayload(p []byte) (Credential, []byte, string, error) {
	var w changePassphraseWire
	if err := ssh.Unmarshal(p, &w); err != nil {
		return Credential{}, nil, "", err
	}
	cred, err := credentialFromWire(w.CredKind, w.Credential)
	return cred, w.NewPass, w.NewMnemonic, err
}

// BuildSetRecoveryPayload is the OpSetRecovery request body.
func BuildSetRecoveryPayload(cred Credential, mnemonic string) []byte {
	kind, b := credentialWire(cred)
	return ssh.Marshal(setRecoveryWire{CredKind: kind, Credential: b, Mnemonic: mnemonic})
}

func parseSetRecoveryPayload(p []byte) (Credential, string, error) {
	var w setRecoveryWire
	if err := ssh.Unmarshal(p, &w); err != nil {
		return Credential{}, "", err
	}
	cred, err := credentialFromWire(w.CredKind, w.Credential)
	return cred, w.Mnemonic, err
}
