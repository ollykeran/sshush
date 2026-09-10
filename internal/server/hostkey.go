package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// EnsureHostKey makes path usable as the server's host key, generating a new
// ed25519 key there if the file does not exist yet. It reports whether it created
// one.
//
// The host key is what an SSH client pins in known_hosts, so it has to be the same
// key on every start: a server that generated a fresh one each time would greet
// every returning client with the host-key-changed warning.
func EnsureHostKey(path string) (created bool, err error) {
	if _, err := os.Stat(path); err == nil {
		if err := checkHostKey(path); err != nil {
			return false, err
		}
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("server: host key %s: %w", path, err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, fmt.Errorf("server: create host key directory %s: %w", dir, err)
	}
	keyPEM, err := generateHostKeyPEM()
	if err != nil {
		return false, err
	}
	// O_EXCL rather than a plain create: two servers starting at once must not
	// each write a key and hand out different identities.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, checkHostKey(path)
		}
		return false, fmt.Errorf("server: write host key %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(keyPEM); err != nil {
		return false, fmt.Errorf("server: write host key %s: %w", path, err)
	}
	return true, nil
}

// checkHostKey reports whether the file at path holds a private key the server can
// actually use, so a broken one is named at startup rather than at the first
// connection.
func checkHostKey(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("server: read host key %s: %w", path, err)
	}
	if _, err := ssh.ParsePrivateKey(data); err != nil {
		return fmt.Errorf("server: host key %s is not a usable private key: %w", path, err)
	}
	return nil
}

func generateHostKeyPEM() ([]byte, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("server: generate ed25519 key: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, fmt.Errorf("server: marshal host key: %w", err)
	}
	return pem.EncodeToMemory(block), nil
}

// HostKeyFingerprint returns the SHA256 fingerprint of the host key at path, in the
// form OpenSSH prints when it asks about an unknown host, so a user can compare the
// two without trusting the prompt.
func HostKeyFingerprint(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("server: read host key %s: %w", path, err)
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return "", fmt.Errorf("server: host key %s is not a usable private key: %w", path, err)
	}
	return ssh.FingerprintSHA256(signer.PublicKey()), nil
}
