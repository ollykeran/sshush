package vault

import (
	"time"
)

// Init creates the vault: generates salt, derives master key from passphrase,
// encrypts the canary string, and stores metadata in the store.
// The vault is ready for Unlock(passphrase) and Add() after this.
func Init(store *VaultStore, passphrase []byte) error {
	salt, err := GenerateSalt()
	if err != nil {
		return err
	}
	masterKey := DeriveKey(passphrase, salt)
	defer wipe(masterKey)
	canaryCipher, err := encryptBlob(masterKey, []byte(canaryPlaintext))
	if err != nil {
		return err
	}
	store.SetMetadata(&VaultMetadata{
		Salt:      salt,
		Canary:    canaryCipher,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	return store.Save()
}
