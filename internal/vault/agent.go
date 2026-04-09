package vault

import (
	"encoding/binary"
	"strings"
	"sync"

	"github.com/ollykeran/sshush/internal/kdf"
	"github.com/ollykeran/sshush/internal/openssh"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// ExtensionAddKeyOpts is the extension type for adding a key with autoload option.
// Payload: 4-byte big-endian PEM length, PEM bytes, 1 byte autoload (0 or 1).
const ExtensionAddKeyOpts = "add-key-opts"

// ExtensionVaultSessionLoad loads a non-autoload identity into the current agent session
// (see sessionAutoload0). Payload: UTF-8 SHA256 fingerprint string (same form as ssh.FingerprintSHA256).
const ExtensionVaultSessionLoad = "vault-session-load"

// ExtensionVaultSetAutoload sets Identity.Autoload on disk. Payload: 4-byte big-endian
// fingerprint length, UTF-8 fingerprint bytes, 1 byte (0 = off, 1 = on).
const ExtensionVaultSetAutoload = "vault-set-autoload"

// VaultAgent implements sshagent.ExtendedAgent, storing private keys encrypted
// in a JSON vault. Master key is held in memory when unlocked and wiped on Lock().
type VaultAgent struct {
	store             *VaultStore
	mu                sync.RWMutex
	masterKey         []byte              // nil when locked; wiped on Lock()
	sessionAutoload0  map[string]struct{}  // fingerprints added this run with autoload=0 (visible until restart)
}

// NewVaultAgent returns a VaultAgent that uses the given store. The vault is
// locked (masterKey nil) until Unlock() is called.
func NewVaultAgent(store *VaultStore) *VaultAgent {
	return &VaultAgent{store: store, sessionAutoload0: make(map[string]struct{})}
}

// List returns identities that are autoload=1 or in the session set (added this run with autoload=0).
// When locked (no master key), returns an empty list per SSH agent protocol (locked agents return empty).
func (a *VaultAgent) List() ([]*sshagent.Key, error) {
	a.mu.RLock()
	if a.masterKey == nil {
		a.mu.RUnlock()
		return nil, nil
	}
	sessionFPs := make([]string, 0, len(a.sessionAutoload0))
	for fp := range a.sessionAutoload0 {
		sessionFPs = append(sessionFPs, fp)
	}
	a.mu.RUnlock()
	sessionSet := make(map[string]struct{})
	for _, fp := range sessionFPs {
		sessionSet[fp] = struct{}{}
	}
	rows, err := a.store.ListIdentitiesForAgent(sessionSet)
	if err != nil {
		return nil, err
	}
	keys := make([]*sshagent.Key, len(rows))
	for i := range rows {
		keys[i] = &sshagent.Key{Blob: rows[i].PublicKey, Comment: rows[i].Comment}
	}
	return keys, nil
}

// Add encrypts the private key and adds it to the store with autoload=false,
// and adds the fingerprint to the session set so the key is visible until restart.
func (a *VaultAgent) Add(key sshagent.AddedKey) error {
	return a.addKeyWithAutoload(key, false)
}

// addKeyWithAutoload adds the key with the given autoload.
// When autoload is false, the fingerprint is added to sessionAutoload0 so the key is visible until restart.
func (a *VaultAgent) addKeyWithAutoload(key sshagent.AddedKey, autoload bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	signer, err := ssh.NewSignerFromKey(key.PrivateKey)
	if err != nil {
		return err
	}
	pub := signer.PublicKey()
	pubBlob := pub.Marshal()
	fp := fingerprint(pub)
	if a.masterKey == nil {
		return errLocked
	}
	plain, err := marshalPrivateKey(key.PrivateKey)
	if err != nil {
		return err
	}
	encrypted, err := encryptBlob(a.masterKey, plain)
	if err != nil {
		return err
	}
	wipe(plain)
	id := Identity{
		Fingerprint:   fp,
		PublicKey:     pubBlob,
		EncryptedBlob: encrypted,
		Comment:       key.Comment,
		Autoload:      autoload,
	}
	if err := a.store.AddOrReplaceIdentity(id); err != nil {
		return err
	}
	if err := a.store.Save(); err != nil {
		return err
	}
	if !autoload {
		a.sessionAutoload0[fp] = struct{}{}
	}
	return nil
}

// Remove deletes the identity with the given public key.
func (a *VaultAgent) Remove(key ssh.PublicKey) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.masterKey == nil {
		return errLocked
	}
	fp := fingerprint(key)
	delete(a.sessionAutoload0, fp)
	a.store.RemoveIdentity(fp)
	return a.store.Save()
}

// RemoveAll deletes all identities.
func (a *VaultAgent) RemoveAll() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.masterKey == nil {
		return errLocked
	}
	a.store.RemoveAllIdentities()
	a.sessionAutoload0 = make(map[string]struct{})
	return a.store.Save()
}

// Lock wipes the master key from memory; Sign will fail until Unlock.
func (a *VaultAgent) Lock(passphrase []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.masterKey != nil {
		wipe(a.masterKey)
		a.masterKey = nil
	}
	return nil
}

// UnlockWithRecovery restores the master key using the recovery phrase and marks the vault unlocked.
func (a *VaultAgent) UnlockWithRecovery(mnemonic string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	meta := a.store.GetMetadata()
	if meta == nil || len(meta.RecoverySalt) == 0 || len(meta.WrappedMasterKey) == 0 {
		return errWrongPassphrase
	}
	recoveryKey := kdf.DeriveKey([]byte(mnemonic), meta.RecoverySalt)
	defer wipe(recoveryKey)
	masterKey, err := decryptBlob(recoveryKey, meta.WrappedMasterKey)
	if err != nil {
		return errWrongPassphrase
	}
	if a.masterKey != nil {
		wipe(a.masterKey)
	}
	a.masterKey = masterKey
	return nil
}

// Unlock derives the master key from passphrase and verifies the canary.
func (a *VaultAgent) Unlock(passphrase []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	meta := a.store.GetMetadata()
	if meta == nil || len(meta.Salt) == 0 || len(meta.Canary) == 0 {
		return errWrongPassphrase
	}
	masterKey := kdf.DeriveKey(passphrase, meta.Salt)
	canaryPlain, err := decryptBlob(masterKey, meta.Canary)
	if err != nil || !kdf.ConstantTimeCompare(canaryPlain, []byte(canaryPlaintext)) {
		wipe(masterKey)
		return errWrongPassphrase
	}
	wipe(canaryPlain)
	if a.masterKey != nil {
		wipe(a.masterKey)
	}
	a.masterKey = masterKey
	return nil
}

// Sign decrypts the key blob, signs data, then zeros the decrypted buffer.
// Only allows signing for keys that are listed (autoload=true or in session set).
func (a *VaultAgent) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	a.mu.RLock()
	if a.masterKey == nil {
		a.mu.RUnlock()
		return nil, errLocked
	}
	fp := fingerprint(key)
	encrypted, autoload, found := a.store.GetIdentity(fp)
	_, inSession := a.sessionAutoload0[fp]
	a.mu.RUnlock()
	if !found {
		return nil, errKeyNotFound
	}
	if !autoload && !inSession {
		return nil, errKeyNotFound
	}
	plain, err := decryptBlob(a.masterKey, encrypted)
	if err != nil {
		return nil, err
	}
	defer wipe(plain)
	priv, err := unmarshalPrivateKey(plain, key.Type())
	if err != nil {
		return nil, err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, err
	}
	return signer.Sign(nil, data)
}

// Signers is not supported: we do not keep decrypted keys in memory.
func (a *VaultAgent) Signers() ([]ssh.Signer, error) {
	return nil, errNotImplemented
}

// SignWithFlags implements ExtendedAgent (task 3.2); stub.
func (a *VaultAgent) SignWithFlags(key ssh.PublicKey, data []byte, flags sshagent.SignatureFlags) (*ssh.Signature, error) {
	return a.Sign(key, data)
}

// ExtensionVaultLocked is the extension type for querying whether the vault is locked.
// Response: one byte, 1 if locked (masterKey == nil), 0 if unlocked.
const ExtensionVaultLocked = "vault-locked"

// sessionLoad marks a non-autoload identity as visible in this session (until daemon restart).
func (a *VaultAgent) sessionLoad(fp string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.masterKey == nil {
		return errLocked
	}
	_, autoload, found := a.store.GetIdentity(fp)
	if !found {
		return errKeyNotFound
	}
	if autoload {
		return nil
	}
	a.sessionAutoload0[fp] = struct{}{}
	return nil
}

// setIdentityAutoload persists Autoload for an identity; clears sessionAutoload0 when enabling autoload.
func (a *VaultAgent) setIdentityAutoload(fp string, on bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.masterKey == nil {
		return errLocked
	}
	ids := a.store.AllIdentities()
	var id Identity
	ok := false
	for i := range ids {
		if ids[i].Fingerprint == fp {
			id = ids[i]
			ok = true
			break
		}
	}
	if !ok {
		return errKeyNotFound
	}
	if id.Autoload == on {
		return nil
	}
	id.Autoload = on
	if err := a.store.AddOrReplaceIdentity(id); err != nil {
		return err
	}
	if err := a.store.Save(); err != nil {
		return err
	}
	if on {
		delete(a.sessionAutoload0, fp)
	}
	return nil
}

// Extension implements ExtendedAgent. Supports "vault-locked", "unlock-recovery", "add-key-opts",
// "vault-session-load", and "vault-set-autoload".
func (a *VaultAgent) Extension(extensionType string, contents []byte) ([]byte, error) {
	if extensionType == ExtensionVaultLocked {
		a.mu.RLock()
		locked := a.masterKey == nil
		a.mu.RUnlock()
		if locked {
			return []byte{1}, nil
		}
		return []byte{0}, nil
	}
	if extensionType == "unlock-recovery" {
		mnemonic := strings.Join(strings.Fields(strings.TrimSpace(string(contents))), " ")
		if err := a.UnlockWithRecovery(mnemonic); err != nil {
			return nil, err
		}
		return []byte("ok"), nil
	}
	if extensionType == ExtensionAddKeyOpts {
		if len(contents) < 5 {
			return nil, errExtensionPayload
		}
		pemLen := binary.BigEndian.Uint32(contents[:4])
		if int(pemLen) > len(contents)-5 {
			return nil, errExtensionPayload
		}
		pem := contents[4 : 4+pemLen]
		autoloadByte := contents[4+pemLen]
		autoload := autoloadByte == 1
		if autoloadByte != 0 && autoloadByte != 1 {
			return nil, errExtensionPayload
		}
		key, err := ssh.ParseRawPrivateKey(pem)
		if err != nil {
			return nil, err
		}
		comment := ""
		if parsed, err := openssh.ParsePrivateKeyBlob(pem); err == nil && parsed.Comment != "" {
			comment = parsed.Comment
		}
		addedKey := sshagent.AddedKey{PrivateKey: key, Comment: comment}
		if err := a.addKeyWithAutoload(addedKey, autoload); err != nil {
			return nil, err
		}
		return []byte("ok"), nil
	}
	if extensionType == ExtensionVaultSessionLoad {
		fp := strings.TrimSpace(string(contents))
		if fp == "" {
			return nil, errExtensionPayload
		}
		if err := a.sessionLoad(fp); err != nil {
			return nil, err
		}
		return []byte("ok"), nil
	}
	if extensionType == ExtensionVaultSetAutoload {
		if len(contents) < 5 {
			return nil, errExtensionPayload
		}
		fpLen64 := binary.BigEndian.Uint32(contents[:4])
		if int(fpLen64)+5 != len(contents) {
			return nil, errExtensionPayload
		}
		fpLen := int(fpLen64)
		fp := string(contents[4 : 4+fpLen])
		flag := contents[4+fpLen]
		if flag != 0 && flag != 1 {
			return nil, errExtensionPayload
		}
		if err := a.setIdentityAutoload(fp, flag == 1); err != nil {
			return nil, err
		}
		return []byte("ok"), nil
	}
	return nil, sshagent.ErrExtensionUnsupported
}

// Ensure VaultAgent implements both interfaces at compile time.
var (
	_ sshagent.Agent         = (*VaultAgent)(nil)
	_ sshagent.ExtendedAgent = (*VaultAgent)(nil)
)
