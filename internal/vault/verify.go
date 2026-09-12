package vault

import "github.com/ollykeran/sshush/internal/kdf"

// VerifyPassphrase reports whether passphrase opens the vault in store, without
// unlocking anything: the master key it derives is wiped before it returns, and no
// agent is asked or changed. It is for proving someone knows the passphrase — as
// sshush server's password authentication does — where unlocking the agent would
// be a side effect nobody asked for.
//
// Checking costs one full key derivation, correct passphrase or not. A vault that
// has not been initialized has nothing to check against, and accepts nothing.
func VerifyPassphrase(store *VaultStore, passphrase []byte) bool {
	masterKey, err := masterKeyFromPassphrase(store.GetMetadata(), passphrase)
	if err != nil {
		return false
	}
	wipe(masterKey)
	return true
}

// masterKeyFromPassphrase derives the master key from passphrase and proves it
// against the vault's canary, returning errWrongPassphrase for a passphrase that
// does not open the vault or a vault with no canary to open. The caller owns the
// returned key and must wipe it.
func masterKeyFromPassphrase(meta *VaultMetadata, passphrase []byte) ([]byte, error) {
	if meta == nil || len(meta.Salt) == 0 || len(meta.Canary) == 0 {
		return nil, errWrongPassphrase
	}
	masterKey := kdf.DeriveKey(passphrase, meta.Salt)
	canaryPlain, err := decryptBlob(masterKey, meta.Canary)
	if err != nil || !kdf.ConstantTimeCompare(canaryPlain, []byte(canaryPlaintext)) {
		wipe(masterKey)
		return nil, errWrongPassphrase
	}
	wipe(canaryPlain)
	return masterKey, nil
}
