package vault

import (
	"github.com/ollykeran/sshush/internal/kdf"
	"github.com/tyler-smith/go-bip39"
)

// GenerateRecoveryMnemonic returns a new 24-word BIP-39 mnemonic (256-bit entropy).
func GenerateRecoveryMnemonic() (string, error) {
	entropy, err := bip39.NewEntropy(256)
	if err != nil {
		return "", err
	}
	return bip39.NewMnemonic(entropy)
}

// EnableRecovery stores the wrapped master key in the store so UnlockWithRecovery
// can restore access. The vault must already be initialized. masterKey is the
// current unlocked master key (not copied; caller keeps ownership).
func EnableRecovery(store *VaultStore, masterKey []byte, mnemonic string) error {
	recoverySalt, err := kdf.GenerateSalt()
	if err != nil {
		return err
	}
	recoveryKey := kdf.DeriveKey([]byte(mnemonic), recoverySalt)
	defer wipe(recoveryKey)
	wrapped, err := encryptBlob(recoveryKey, masterKey)
	if err != nil {
		return err
	}
	meta := store.GetMetadata()
	if meta == nil {
		return errWrongPassphrase
	}
	meta.RecoverySalt = recoverySalt
	meta.WrappedMasterKey = wrapped
	store.SetMetadata(meta)
	return store.Save()
}

// EnableRecoveryWithPassphrase reads salt from the store, derives the master key
// from passphrase, then enables recovery with the given mnemonic. Use right after
// Init when creating a vault without --no-recovery.
func EnableRecoveryWithPassphrase(store *VaultStore, passphrase []byte, mnemonic string) error {
	meta := store.GetMetadata()
	if meta == nil || len(meta.Salt) == 0 {
		return errWrongPassphrase
	}
	masterKey := kdf.DeriveKey(passphrase, meta.Salt)
	defer wipe(masterKey)
	return EnableRecovery(store, masterKey, mnemonic)
}
