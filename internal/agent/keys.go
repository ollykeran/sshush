package agent

import (
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/ollykeran/sshush/internal/openssh"
	"github.com/ollykeran/sshush/internal/style"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// filepathRegistry maps key fingerprints to their original file paths on disk.
// Used by the edit command and other operations that need to locate the source file.
var filepathRegistry sync.Map

// RegisterFilepath records the file path for a key fingerprint.
func RegisterFilepath(fingerprint, path string) {
	filepathRegistry.Store(fingerprint, path)
}

// GetFilepath returns the file path for a key fingerprint, or empty string if not found.
func GetFilepath(fingerprint string) string {
	if v, ok := filepathRegistry.Load(fingerprint); ok {
		return v.(string)
	}
	return ""
}

// ResolveFilepath attempts to find the source file path for a key by fingerprint.
// For non-vault agents, it falls back to parsing each path in cfgPaths to match.
func ResolveFilepath(fingerprint string, cfgPaths []string) string {
	if fp := GetFilepath(fingerprint); fp != "" {
		return fp
	}
	for _, path := range cfgPaths {
		pubKey, _, _, err := ParseKeyFromPath(path)
		if err != nil {
			continue
		}
		if ssh.FingerprintSHA256(pubKey) == fingerprint {
			RegisterFilepath(fingerprint, path)
			return path
		}
	}
	return ""
}

// ParseKeyFromPath reads a private key file and returns the public key,
// comment, and raw private key without adding to any keyring.
func ParseKeyFromPath(path string) (ssh.PublicKey, string, interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", nil, fmt.Errorf("agent: read key %s: %w", path, err)
	}
	if block, _ := pem.Decode(data); block != nil {
		if block.Headers["Proc-Type"] == "4,ENCRYPTED" {
			return nil, "", nil, openssh.ErrEncryptedPrivateKey
		}
	}
	parsed, err := openssh.ParsePrivateKeyBlob(data)
	if errors.Is(err, openssh.ErrEncryptedPrivateKey) {
		return nil, "", nil, err
	}
	var openComment *openssh.ParsedKey
	if err == nil {
		openComment = parsed
	}
	key, err := ssh.ParseRawPrivateKey(data)
	if err != nil {
		var pm *ssh.PassphraseMissingError
		if errors.As(err, &pm) {
			return nil, "", nil, openssh.ErrEncryptedPrivateKey
		}
		return nil, "", nil, fmt.Errorf("agent: parse private key %s: %w", path, err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, "", nil, fmt.Errorf("agent: create signer for %s: %w", path, err)
	}
	comment := filepath.Base(path)
	if openComment != nil && openComment.Comment != "" {
		comment = openComment.Comment
	}
	return signer.PublicKey(), comment, key, nil
}

// AddKeyFromPath reads a private key from path and adds it to the keyring.
// The fingerprint-to-filepath mapping is registered for later lookup.
func AddKeyFromPath(keyring sshagent.Agent, path string) error {
	pubKey, comment, key, err := ParseKeyFromPath(path)
	if err != nil {
		return err
	}
	fp := ssh.FingerprintSHA256(pubKey)
	RegisterFilepath(fp, path)
	return keyring.Add(sshagent.AddedKey{PrivateKey: key, Comment: comment})
}

// LoadKeys reads each path and adds keys to the keyring. Errors for a path are
// written to errOut and skipped; the first fatal error is returned.
func LoadKeys(keyring sshagent.Agent, paths []string, errOut io.Writer) error {
	for _, path := range paths {
		if err := AddKeyFromPath(keyring, path); err != nil {
			fmt.Fprintln(errOut, style.Err(err.Error()))
		}
	}
	return nil
}
