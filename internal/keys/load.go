package keys

import (
	"errors"
	"fmt"
	"os"

	"github.com/ollykeran/sshush/internal/openssh"
	ssh "golang.org/x/crypto/ssh"
)

// LoadKeyMaterial reads a key file and returns parsed metadata, raw key, and signer.
func LoadKeyMaterial(path string) (*openssh.ParsedKey, interface{}, ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("keys: read key %s: %w", path, err)
	}

	parsed, err := openssh.ParsePrivateKeyBlob(data)
	if errors.Is(err, openssh.ErrEncryptedPrivateKey) {
		return nil, nil, nil, fmt.Errorf("keys: %s: encrypted keys not supported", path)
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("keys: parse %s: not an unencrypted OpenSSH private key file", path)
	}

	rawKey, err := ssh.ParseRawPrivateKey(data)
	if err != nil {
		var pm *ssh.PassphraseMissingError
		if errors.As(err, &pm) {
			return nil, nil, nil, fmt.Errorf("keys: %s: encrypted keys not supported", path)
		}
		return nil, nil, nil, fmt.Errorf("keys: parse %s: %w", path, err)
	}

	signer, err := ssh.NewSignerFromKey(rawKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("keys: create signer for %s: %w", path, err)
	}

	return parsed, rawKey, signer, nil
}
