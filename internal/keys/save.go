package keys

import (
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	ssh "golang.org/x/crypto/ssh"
)

// SavePair writes the private key with 0600 permissions and the public key
// (.pub) with 0644 permissions to the specified directory.
func SavePair(dir, filename string, privPEM, pubAuth []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	privPath := filepath.Join(dir, filename)
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}

	pubPath := privPath + ".pub"
	if err := os.WriteFile(pubPath, pubAuth, 0o644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}

	return nil
}

// SaveWithComment writes private key material with an updated comment and
// updates the .pub companion file when it exists.
func SaveWithComment(rawKey interface{}, comment, privPath string) error {
	block, err := ssh.MarshalPrivateKey(rawKey, comment)
	if err != nil {
		return fmt.Errorf("marshal key: %w", err)
	}

	privPEM := pem.EncodeToMemory(block)
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}

	pubPath := privPath + ".pub"
	if _, err := os.Stat(pubPath); err == nil {
		signer, err := ssh.NewSignerFromKey(rawKey)
		if err != nil {
			return fmt.Errorf("create signer: %w", err)
		}
		pubLine := FormatPublicKey(signer, comment)
		if err := os.WriteFile(pubPath, []byte(pubLine), 0o644); err != nil {
			return fmt.Errorf("update .pub: %w", err)
		}
	}

	return nil
}
