package kdf

import (
	"crypto/rand"
	"crypto/subtle"

	"golang.org/x/crypto/argon2"
)

const (
	argon2Time    = 3
	argon2Memory  = 64 * 1024 // 64 MiB
	argon2Threads = 1
	// KeyLen is the byte length of DeriveKey output (AES-256).
	KeyLen = 32
)

// DeriveKey derives a 32-byte key from passphrase and salt using Argon2id.
func DeriveKey(passphrase, salt []byte) []byte {
	return argon2.IDKey(passphrase, salt, argon2Time, argon2Memory, argon2Threads, KeyLen)
}

// GenerateSalt returns 16 random bytes for use as KDF salt.
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return salt, nil
}

// ConstantTimeCompare returns true if a and b are equal. Use for canary comparison.
func ConstantTimeCompare(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}
