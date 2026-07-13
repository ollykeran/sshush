package agent

import (
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ollykeran/sshush/internal/openssh"
	"github.com/ollykeran/sshush/internal/style"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// ParseKeyFromPath reads a private key file and returns the public key,
// comment, and raw private key without adding to any keyring.
func ParseKeyFromPath(path string) (ssh.PublicKey, string, interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", nil, err
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
		return nil, "", nil, err
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, "", nil, err
	}
	comment := filepath.Base(path)
	if openComment != nil && openComment.Comment != "" {
		comment = openComment.Comment
	}
	return signer.PublicKey(), comment, key, nil
}

// AddKeyFromPath reads a private key from path and adds it to the keyring.
func AddKeyFromPath(keyring sshagent.Agent, path string) error {
	_, comment, key, err := ParseKeyFromPath(path)
	if err != nil {
		return err
	}
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
