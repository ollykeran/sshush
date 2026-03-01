package keys

import (
	"fmt"
	"os"

	"github.com/ollykeran/sshush/internal/openssh"
	ssh "golang.org/x/crypto/ssh"
)

// LoadKeyMaterial reads a key file and returns parsed metadata, raw key, and signer.
func LoadKeyMaterial(path string) (*openssh.ParsedKey, interface{}, ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read key: %w", err)
	}

	parsed, err := openssh.ParsePrivateKeyBlob(data)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("encrypted keys not supported")
	}

	rawKey, err := ssh.ParseRawPrivateKey(data)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse key: %w", err)
	}

	signer, err := ssh.NewSignerFromKey(rawKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create signer: %w", err)
	}

	return parsed, rawKey, signer, nil
}
