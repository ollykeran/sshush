package tui

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// GenerateKey creates an SSH key pair of the specified type and returns the
// PEM-encoded private key and authorized_keys-format public key.
func GenerateKey(keyType string, bits int, comment string) (privPEM []byte, pubAuth []byte, err error) {
	var rawKey interface{}

	switch keyType {
	case "ed25519":
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, nil, fmt.Errorf("generate ed25519: %w", err)
		}
		rawKey = priv

	case "rsa":
		if bits == 0 {
			bits = 4096
		}
		priv, err := rsa.GenerateKey(rand.Reader, bits)
		if err != nil {
			return nil, nil, fmt.Errorf("generate rsa-%d: %w", bits, err)
		}
		rawKey = priv

	case "ecdsa":
		var curve elliptic.Curve
		switch bits {
		case 256, 0:
			curve = elliptic.P256()
		case 384:
			curve = elliptic.P384()
		case 521:
			curve = elliptic.P521()
		default:
			return nil, nil, fmt.Errorf("unsupported ecdsa curve size: %d", bits)
		}
		priv, err := ecdsa.GenerateKey(curve, rand.Reader)
		if err != nil {
			return nil, nil, fmt.Errorf("generate ecdsa-%d: %w", bits, err)
		}
		rawKey = priv

	default:
		return nil, nil, fmt.Errorf("unsupported key type: %s", keyType)
	}

	block, err := ssh.MarshalPrivateKey(rawKey, comment)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal private key: %w", err)
	}
	privPEM = pem.EncodeToMemory(block)

	signer, err := ssh.NewSignerFromKey(rawKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create signer: %w", err)
	}
	pubLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	pubAuth = []byte(pubLine + " " + comment + "\n")

	return privPEM, pubAuth, nil
}

// SaveKeyPair writes the private key with 0600 permissions and the public key
// (.pub) with 0644 permissions to the specified directory.
func SaveKeyPair(dir, filename string, privPEM, pubAuth []byte) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	privPath := filepath.Join(dir, filename)
	if err := os.WriteFile(privPath, privPEM, 0600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}

	pubPath := privPath + ".pub"
	if err := os.WriteFile(pubPath, pubAuth, 0644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}

	return nil
}
