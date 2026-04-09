package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"

	"github.com/ollykeran/sshush/internal/kdf"
)

const (
	gcmIVSize = 12
)

// encryptBlob encrypts plain with AES-256-GCM using masterKey. A random IV is
// prepended to the returned ciphertext (iv + ciphertext + tag).
func encryptBlob(masterKey, plain []byte) ([]byte, error) {
	if len(masterKey) != kdf.KeyLen {
		return nil, errors.New("vault: master key must be 32 bytes")
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	iv := make([]byte, gcmIVSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}
	ciphertext := aead.Seal(iv, iv, plain, nil)
	return ciphertext, nil
}

// decryptBlob decrypts ciphertext (iv + ciphertext + tag) with masterKey.
func decryptBlob(masterKey, ciphertext []byte) ([]byte, error) {
	if len(masterKey) != kdf.KeyLen {
		return nil, errors.New("vault: master key must be 32 bytes")
	}
	if len(ciphertext) < gcmIVSize {
		return nil, errors.New("vault: ciphertext too short")
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	iv, ct := ciphertext[:gcmIVSize], ciphertext[gcmIVSize:]
	plain, err := aead.Open(nil, iv, ct, nil)
	if err != nil {
		return nil, err
	}
	return plain, nil
}

// wipe overwrites b with zeros. Call before discarding sensitive slices.
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

const canaryPlaintext = "SSHUSH_VALID"

var (
	errNotImplemented  = errors.New("vault: not implemented")
	errLocked          = errors.New("vault is locked")
	errWrongPassphrase = errors.New("vault: wrong passphrase")
	errKeyNotFound     = errors.New("vault: key not found")
	errExtensionPayload = errors.New("vault: invalid extension payload")
)
