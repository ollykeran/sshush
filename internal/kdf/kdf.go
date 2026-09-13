package kdf

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"

	"golang.org/x/crypto/argon2"
)

const (
	// KeyLen is the byte length of DeriveKey output (AES-256).
	KeyLen = 32
)

var (
	argon2Time    uint32 = 3
	argon2Memory  uint32 = 64 * 1024 // 64 MiB
	argon2Threads uint8  = 1
)

// DeriveKey derives a 32-byte key from passphrase and salt using Argon2id.
func DeriveKey(passphrase, salt []byte) []byte {
	return argon2.IDKey(passphrase, salt, argon2Time, argon2Memory, argon2Threads, KeyLen)
}

// SetInsecureFastParamsForTesting weakens the Argon2id cost parameters to
// values fast enough for unit tests. Call it only from a package's
// TestMain — never from production code paths — since it makes the KDF
// trivially breakable.
func SetInsecureFastParamsForTesting() {
	argon2Time = 1
	argon2Memory = 8 * 1024
	argon2Threads = 1
}

// GenerateSalt returns 16 random bytes for use as KDF salt.
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("kdf: generate salt: %w", err)
	}
	return salt, nil
}

// ConstantTimeCompare returns true if a and b are equal. Use for canary comparison.
func ConstantTimeCompare(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}
