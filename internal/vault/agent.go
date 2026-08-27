package vault

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ollykeran/sshush/internal/kdf"
	"github.com/ollykeran/sshush/internal/openssh"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// ExtensionAddKeyOpts is the extension type for adding a key with autoload option.
// Payload: 4-byte big-endian PEM length, PEM bytes, 1 byte autoload (0 or 1).
const ExtensionAddKeyOpts = "add-key-opts"

// ExtensionQuery is the OpenSSH-defined extension that lists supported extension names.
const ExtensionQuery = "query"

// ExtensionUnlockRecovery unlocks the vault with the BIP-39 recovery phrase.
// Payload: UTF-8 space-separated mnemonic words.
const ExtensionUnlockRecovery = "unlock-recovery"

// ExtensionVaultSessionLoad loads a non-autoload identity into the current agent session
// (see sessionAutoload0). Payload: UTF-8 SHA256 fingerprint string (same form as ssh.FingerprintSHA256).
const ExtensionVaultSessionLoad = "vault-session-load"

// ExtensionVaultSessionUnload hides an identity from the current agent session without
// deleting it or touching its persisted autoload flag (see sessionUnload). Payload:
// UTF-8 SHA256 fingerprint string (same form as ssh.FingerprintSHA256).
const ExtensionVaultSessionUnload = "vault-session-unload"

// ExtensionVaultSetAutoload sets Identity.Autoload on disk. Payload: 4-byte big-endian
// fingerprint length, UTF-8 fingerprint bytes, 1 byte (0 = off, 1 = on).
const ExtensionVaultSetAutoload = "vault-set-autoload"

// ExtensionVaultSetComment sets Identity.Comment on disk. Payload: 4-byte big-endian
// fingerprint length, UTF-8 fingerprint bytes, 4-byte big-endian comment length, UTF-8 comment.
const ExtensionVaultSetComment = "vault-set-comment"

// VaultAgent implements sshagent.ExtendedAgent, storing private keys encrypted
// in a JSON vault. Master key is held in memory when unlocked and wiped on Lock().
type VaultAgent struct {
	store            *VaultStore
	mu               sync.RWMutex
	masterKey        []byte              // nil when locked; wiped on Lock()
	sessionAutoload0 map[string]struct{} // fingerprints added this run with autoload=0 (visible until restart)
	sessionUnloaded  map[string]struct{} // autoload=1 fingerprints hidden this run only (see sessionUnload)
}

// NewVaultAgent returns a VaultAgent that uses the given store. The vault is
// locked (masterKey nil) until Unlock() is called.
func NewVaultAgent(store *VaultStore) *VaultAgent {
	return &VaultAgent{
		store:            store,
		sessionAutoload0: make(map[string]struct{}),
		sessionUnloaded:  make(map[string]struct{}),
	}
}

// purgeExpired removes identities whose lifetime has elapsed, mirroring the
// x/crypto keyring's lazy drop on List/Sign. No-op when the vault is locked.
func (a *VaultAgent) purgeExpired() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.masterKey == nil {
		return nil
	}
	now := time.Now()
	var expired []string
	for _, id := range a.store.AllIdentities() {
		if !id.ExpiresAt.IsZero() && now.After(id.ExpiresAt) {
			expired = append(expired, id.Fingerprint)
		}
	}
	if len(expired) == 0 {
		return nil
	}
	for _, fp := range expired {
		a.store.RemoveIdentity(fp)
		delete(a.sessionAutoload0, fp)
		delete(a.sessionUnloaded, fp)
	}
	if err := a.store.Save(); err != nil {
		return fmt.Errorf("vault: save store: %w", err)
	}
	return nil
}

// List returns identities that are autoload=1 or in the session set (added this run with autoload=0).
// When locked (no master key), returns an empty list per SSH agent protocol (locked agents return empty).
func (a *VaultAgent) List() ([]*sshagent.Key, error) {
	if err := a.purgeExpired(); err != nil {
		return nil, err
	}
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
	a.mu.RLock()
	hiddenSet := make(map[string]struct{}, len(a.sessionUnloaded))
	for fp := range a.sessionUnloaded {
		hiddenSet[fp] = struct{}{}
	}
	a.mu.RUnlock()
	rows, err := a.store.ListIdentitiesForAgent(sessionSet, hiddenSet)
	if err != nil {
		return nil, fmt.Errorf("vault: list identities: %w", err)
	}
	keys := make([]*sshagent.Key, len(rows))
	for i := range rows {
		keys[i] = &sshagent.Key{Blob: rows[i].PublicKey, Comment: rows[i].Comment}
	}
	return keys, nil
}

// Add encrypts the private key and adds it to the store with autoload=false,
// and adds the fingerprint to the session set so the key is visible until restart.
// Constraints the vault cannot honor (confirm, constraint extensions, certificates)
// are rejected so clients are never silently mis-served (OpenSSH reply rule).
func (a *VaultAgent) Add(key sshagent.AddedKey) error {
	if key.ConfirmBeforeUse {
		return errors.New("agent: confirm before use constraint is not supported")
	}
	if len(key.ConstraintExtensions) > 0 {
		return errors.New("agent: constraint extensions are present but not supported")
	}
	if key.Certificate != nil {
		return errors.New("agent: certificate identities are not supported")
	}
	return a.addKeyWithAutoload(key, false, "")
}

// addKeyWithAutoload adds the key with the given autoload.
// When autoload is false, the fingerprint is added to sessionAutoload0 so the key is visible until restart.
// A nonzero LifetimeSecs on key is honored: the identity expires and is lazily dropped.
func (a *VaultAgent) addKeyWithAutoload(key sshagent.AddedKey, autoload bool, keyFilepath string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	signer, err := ssh.NewSignerFromKey(key.PrivateKey)
	if err != nil {
		return fmt.Errorf("vault: create signer: %w", err)
	}
	pub := signer.PublicKey()
	pubBlob := pub.Marshal()
	fp := fingerprint(pub)
	if a.masterKey == nil {
		return errLocked
	}
	plain, err := marshalPrivateKey(key.PrivateKey)
	if err != nil {
		return fmt.Errorf("vault: marshal private key: %w", err)
	}
	encrypted, err := encryptBlob(a.masterKey, plain)
	if err != nil {
		return fmt.Errorf("vault: encrypt key blob: %w", err)
	}
	wipe(plain)
	id := Identity{
		Fingerprint:   fp,
		PublicKey:     pubBlob,
		EncryptedBlob: encrypted,
		Comment:       key.Comment,
		Filepath:      keyFilepath,
		Autoload:      autoload,
	}
	if key.LifetimeSecs > 0 {
		id.ExpiresAt = time.Now().UTC().Add(time.Duration(key.LifetimeSecs) * time.Second)
	}
	if err := a.store.AddOrReplaceIdentity(id); err != nil {
		return fmt.Errorf("vault: store identity: %w", err)
	}
	if err := a.store.Save(); err != nil {
		return fmt.Errorf("vault: save store: %w", err)
	}
	if !autoload {
		a.sessionAutoload0[fp] = struct{}{}
	}
	return nil
}

// Remove deletes the identity with the given public key. Fails if the key is not
// present (OpenSSH reply rule).
func (a *VaultAgent) Remove(key ssh.PublicKey) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.masterKey == nil {
		return errLocked
	}
	fp := fingerprint(key)
	removed := a.store.RemoveIdentity(fp)
	delete(a.sessionAutoload0, fp)
	delete(a.sessionUnloaded, fp)
	if !removed {
		return errKeyNotFound
	}
	if err := a.store.Save(); err != nil {
		return fmt.Errorf("vault: save store: %w", err)
	}
	return nil
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
	a.sessionUnloaded = make(map[string]struct{})
	if err := a.store.Save(); err != nil {
		return fmt.Errorf("vault: save store: %w", err)
	}
	return nil
}

// Lock wipes the master key from memory; Sign will fail until Unlock.
// Fails if the vault is already locked (OpenSSH reply rule).
func (a *VaultAgent) Lock(passphrase []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.masterKey == nil {
		return errAgentLocked
	}
	wipe(a.masterKey)
	a.masterKey = nil
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
		return fmt.Errorf("vault: decrypt with recovery key: %w", err)
	}
	if a.masterKey != nil {
		wipe(a.masterKey)
	}
	a.masterKey = masterKey
	return nil
}

// Unlock derives the master key from passphrase and verifies the canary.
// Fails if the vault is already unlocked (OpenSSH reply rule).
func (a *VaultAgent) Unlock(passphrase []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.masterKey != nil {
		return errAgentNotLocked
	}
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

// signerForKey decrypts the identity blob for key and returns an ssh.Signer.
// Only allows signing for keys that are listed (autoload=true or in session set).
// The decrypted key material is wiped before returning.
func (a *VaultAgent) signerForKey(key ssh.PublicKey) (ssh.Signer, error) {
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
		return nil, fmt.Errorf("vault: decrypt key for signing: %w", err)
	}
	defer wipe(plain)
	priv, err := unmarshalPrivateKey(plain, key.Type())
	if err != nil {
		return nil, fmt.Errorf("vault: unmarshal private key: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, fmt.Errorf("vault: create signer: %w", err)
	}
	return signer, nil
}

// Sign decrypts the key blob, signs data, then zeros the decrypted buffer.
func (a *VaultAgent) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	if err := a.purgeExpired(); err != nil {
		return nil, err
	}
	signer, err := a.signerForKey(key)
	if err != nil {
		return nil, err
	}
	return signer.Sign(nil, data)
}

// Signers is not supported: we do not keep decrypted keys in memory.
func (a *VaultAgent) Signers() ([]ssh.Signer, error) {
	return nil, errNotImplemented
}

// SignWithFlags implements ExtendedAgent, mirroring the OpenSSH / x/crypto
// keyring: flags == 0 signs with the default algorithm; rsa-sha2-256/512 use
// SignWithAlgorithm; any other flags are unsupported.
func (a *VaultAgent) SignWithFlags(key ssh.PublicKey, data []byte, flags sshagent.SignatureFlags) (*ssh.Signature, error) {
	if flags == 0 {
		return a.Sign(key, data)
	}
	if err := a.purgeExpired(); err != nil {
		return nil, err
	}
	signer, err := a.signerForKey(key)
	if err != nil {
		return nil, err
	}
	var algorithm string
	switch flags {
	case sshagent.SignatureFlagRsaSha256:
		algorithm = ssh.KeyAlgoRSASHA256
	case sshagent.SignatureFlagRsaSha512:
		algorithm = ssh.KeyAlgoRSASHA512
	default:
		return nil, fmt.Errorf("vault: unsupported signature flags: %d", flags)
	}
	algorithmSigner, ok := signer.(ssh.AlgorithmSigner)
	if !ok {
		return nil, fmt.Errorf("vault: key does not support non-default signature algorithm: %T", signer)
	}
	return algorithmSigner.SignWithAlgorithm(nil, data, algorithm)
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
	delete(a.sessionUnloaded, fp)
	if autoload {
		return nil
	}
	a.sessionAutoload0[fp] = struct{}{}
	return nil
}

// sessionUnload hides an identity from List()/Sign() for this session only, without
// touching its persisted autoload flag or deleting it from the vault. This is the
// reverse of sessionLoad for the common case: an autoload=1 identity that the TUI's
// Agent tab "removes" from the running agent should disappear from the session (LOADED
// becomes "no" in the Vault tab) but remain in the vault for the next unlock/restart.
// A later sessionLoad call for the same fingerprint clears this and makes it visible
// again immediately.
func (a *VaultAgent) sessionUnload(fp string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.masterKey == nil {
		return errLocked
	}
	_, _, found := a.store.GetIdentity(fp)
	if !found {
		return errKeyNotFound
	}
	delete(a.sessionAutoload0, fp)
	a.sessionUnloaded[fp] = struct{}{}
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
		return fmt.Errorf("vault: store identity: %w", err)
	}
	if err := a.store.Save(); err != nil {
		return fmt.Errorf("vault: save store: %w", err)
	}
	if on {
		delete(a.sessionAutoload0, fp)
	}
	delete(a.sessionUnloaded, fp)
	return nil
}

// setIdentityComment persists a new Comment for an identity without touching key material.
// An empty comment is stored as "".
func (a *VaultAgent) setIdentityComment(fp, comment string) error {
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
	if id.Comment == comment {
		return nil
	}
	id.Comment = comment
	if err := a.store.AddOrReplaceIdentity(id); err != nil {
		return fmt.Errorf("vault: store identity: %w", err)
	}
	if err := a.store.Save(); err != nil {
		return fmt.Errorf("vault: save store: %w", err)
	}
	return nil
}

// Extension implements ExtendedAgent. Supports "vault-locked", "unlock-recovery", "add-key-opts",
// "vault-session-load", "vault-session-unload", "vault-set-autoload", "vault-set-comment", and the
// OpenSSH "query" extension.
func (a *VaultAgent) Extension(extensionType string, contents []byte) ([]byte, error) {
	if extensionType == ExtensionQuery {
		// OpenSSH's ssh-agent returns one SSH string per extension name.
		names := []string{
			ExtensionQuery,
			ExtensionVaultLocked,
			ExtensionUnlockRecovery,
			ExtensionAddKeyOpts,
			ExtensionVaultSessionLoad,
			ExtensionVaultSessionUnload,
			ExtensionVaultSetAutoload,
			ExtensionVaultSetComment,
		}
		var buf bytes.Buffer
		for _, name := range names {
			buf.Write(ssh.Marshal(struct{ Name string }{Name: name}))
		}
		return buf.Bytes(), nil
	}
	if extensionType == ExtensionVaultLocked {
		a.mu.RLock()
		locked := a.masterKey == nil
		a.mu.RUnlock()
		if locked {
			return []byte{1}, nil
		}
		return []byte{0}, nil
	}
	if extensionType == ExtensionUnlockRecovery {
		mnemonic := strings.Join(strings.Fields(strings.TrimSpace(string(contents))), " ")
		if err := a.UnlockWithRecovery(mnemonic); err != nil {
			return nil, fmt.Errorf("vault: unlock recovery: %w", err)
		}
		return []byte("ok"), nil
	}
	if extensionType == ExtensionAddKeyOpts {
		if len(contents) < 5 {
			return nil, fmt.Errorf("vault: add-key-opts: payload too short (%d bytes)", len(contents))
		}
		// Version detection: old format starts with 4-byte PEM length (first byte 0x00 for typical keys);
		// new format (v1) starts with version byte 0x01.
		var pemData []byte
		var autoload bool
		var keyFilepath string
		if contents[0] == 1 && len(contents) >= 10 {
			// Version 1: [1-byte version][4-byte PEM len][PEM][1-byte autoload][4-byte filepath len][filepath]
			pemLen := int(binary.BigEndian.Uint32(contents[1:5]))
			if 5+pemLen > len(contents) {
				return nil, fmt.Errorf("vault: add-key-opts: PEM length %d exceeds payload", pemLen)
			}
			pemData = contents[5 : 5+pemLen]
			autoloadByte := contents[5+pemLen]
			if autoloadByte != 0 && autoloadByte != 1 {
				return nil, fmt.Errorf("vault: add-key-opts: invalid autoload byte %d", autoloadByte)
			}
			autoload = autoloadByte == 1
			fpOffset := 5 + pemLen + 1
			if fpOffset+4 <= len(contents) {
				fpLen := int(binary.BigEndian.Uint32(contents[fpOffset : fpOffset+4]))
				if fpOffset+4+fpLen <= len(contents) {
					keyFilepath = string(contents[fpOffset+4 : fpOffset+4+fpLen])
				}
			}
		} else {
			// Legacy format: [4-byte PEM len][PEM][1-byte autoload]
			pemLen := int(binary.BigEndian.Uint32(contents[:4]))
			if pemLen > len(contents)-5 {
				return nil, fmt.Errorf("vault: add-key-opts: PEM length %d exceeds payload", pemLen)
			}
			pemData = contents[4 : 4+pemLen]
			autoloadByte := contents[4+pemLen]
			if autoloadByte != 0 && autoloadByte != 1 {
				return nil, fmt.Errorf("vault: add-key-opts: invalid autoload byte %d", autoloadByte)
			}
			autoload = autoloadByte == 1
		}
		key, err := ssh.ParseRawPrivateKey(pemData)
		if err != nil {
			return nil, fmt.Errorf("vault: add-key-opts: parse PEM: %w", err)
		}
		comment := ""
		if parsed, err := openssh.ParsePrivateKeyBlob(pemData); err == nil && parsed.Comment != "" {
			comment = parsed.Comment
		}
		addedKey := sshagent.AddedKey{PrivateKey: key, Comment: comment}
		if err := a.addKeyWithAutoload(addedKey, autoload, keyFilepath); err != nil {
			return nil, fmt.Errorf("vault: add-key-opts: %w", err)
		}
		return []byte("ok"), nil
	}
	if extensionType == ExtensionVaultSessionLoad {
		fp := strings.TrimSpace(string(contents))
		if fp == "" {
			return nil, fmt.Errorf("vault: session-load: empty fingerprint")
		}
		if err := a.sessionLoad(fp); err != nil {
			return nil, fmt.Errorf("vault: session-load: %w", err)
		}
		return []byte("ok"), nil
	}
	if extensionType == ExtensionVaultSessionUnload {
		fp := strings.TrimSpace(string(contents))
		if fp == "" {
			return nil, fmt.Errorf("vault: session-unload: empty fingerprint")
		}
		if err := a.sessionUnload(fp); err != nil {
			return nil, fmt.Errorf("vault: session-unload: %w", err)
		}
		return []byte("ok"), nil
	}
	if extensionType == ExtensionVaultSetAutoload {
		if len(contents) < 5 {
			return nil, fmt.Errorf("vault: set-autoload: payload too short (%d bytes)", len(contents))
		}
		fpLen64 := binary.BigEndian.Uint32(contents[:4])
		if int(fpLen64)+5 != len(contents) {
			return nil, fmt.Errorf("vault: set-autoload: fingerprint length %d does not match payload size", fpLen64)
		}
		fpLen := int(fpLen64)
		fp := string(contents[4 : 4+fpLen])
		flag := contents[4+fpLen]
		if flag != 0 && flag != 1 {
			return nil, fmt.Errorf("vault: set-autoload: invalid flag byte %d", flag)
		}
		if err := a.setIdentityAutoload(fp, flag == 1); err != nil {
			return nil, fmt.Errorf("vault: set-autoload: %w", err)
		}
		return []byte("ok"), nil
	}
	if extensionType == ExtensionVaultSetComment {
		if len(contents) < 8 {
			return nil, fmt.Errorf("vault: set-comment: payload too short (%d bytes)", len(contents))
		}
		fpLen := int(binary.BigEndian.Uint32(contents[:4]))
		if 8+fpLen > len(contents) {
			return nil, fmt.Errorf("vault: set-comment: fingerprint length %d exceeds payload", fpLen)
		}
		fp := string(contents[4 : 4+fpLen])
		commentOffset := 4 + fpLen
		commentLen := int(binary.BigEndian.Uint32(contents[commentOffset : commentOffset+4]))
		if commentOffset+4+commentLen != len(contents) {
			return nil, fmt.Errorf("vault: set-comment: comment length %d does not match payload size", commentLen)
		}
		comment := string(contents[commentOffset+4 : commentOffset+4+commentLen])
		if err := a.setIdentityComment(fp, comment); err != nil {
			return nil, fmt.Errorf("vault: set-comment: %w", err)
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
