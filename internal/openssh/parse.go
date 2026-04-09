package openssh

import (
	"encoding/pem"
	"errors"
	"math/big"

	ssh "golang.org/x/crypto/ssh"
)

// https://cvsweb.openbsd.org/src/usr.bin/ssh/PROTOCOL.key?annotate=HEAD
const openssh_auth_magic = "openssh-key-v1\x00"

// ErrNotOpenSSHKey is returned by ParsePrivateKeyBlob when the data is not an unencrypted OpenSSH private key.
// Encrypted OpenSSH keys return [ErrEncryptedPrivateKey] instead.
var ErrNotOpenSSHKey = errors.New("openssh: not an unencrypted OpenSSH private key")

// ErrEncryptedPrivateKey is returned when the file is a passphrase-protected private key.
var ErrEncryptedPrivateKey = errors.New("encrypted private key (passphrase-protected); sshush only supports unencrypted keys")

// ParsedKey holds parsed OpenSSH key metadata.
type ParsedKey struct {
	KeyType    string
	Comment    string
	PublicKey  []byte // SSH wire format public key (e.g. for ssh.ParsePublicKey)
	PrivateKey []byte // PEM-encoded private key
}

// ParsePrivateKeyBlob parses unencrypted OpenSSH private key data.
func ParsePrivateKeyBlob(data []byte) (*ParsedKey, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "OPENSSH PRIVATE KEY" {
		return nil, ErrNotOpenSSHKey
	}
	key := block.Bytes
	if len(key) < len(openssh_auth_magic) || string(key[:len(openssh_auth_magic)]) != openssh_auth_magic {
		return nil, ErrNotOpenSSHKey
	}

	var outer struct {
		CipherName string
		KdfName    string
		KdfOpts    string
		NumKeys    uint32
		PublicKey  []byte
		PrivateKey []byte
		Rest       []byte `ssh:"rest"`
	}
	if err := ssh.Unmarshal(key[len(openssh_auth_magic):], &outer); err != nil || outer.NumKeys != 1 {
		return nil, ErrNotOpenSSHKey
	}
	if outer.CipherName != "none" {
		return nil, ErrEncryptedPrivateKey
	}

	var pk struct {
		Check1  uint32
		Check2  uint32
		Keytype string
		Rest    []byte `ssh:"rest"`
	}
	if err := ssh.Unmarshal(outer.PrivateKey, &pk); err != nil || pk.Check1 != pk.Check2 {
		return nil, ErrNotOpenSSHKey
	}

	out := &ParsedKey{KeyType: pk.Keytype, PublicKey: outer.PublicKey, PrivateKey: outer.PrivateKey}

	switch pk.Keytype {
	case ssh.KeyAlgoED25519:
		var ed struct {
			Pub     []byte
			Priv    []byte
			Comment string
			Pad     []byte `ssh:"rest"`
		}
		if err := ssh.Unmarshal(pk.Rest, &ed); err != nil {
			return nil, ErrNotOpenSSHKey
		}
		out.Comment = ed.Comment
	case ssh.KeyAlgoRSA:
		var rsa struct {
			N       *big.Int
			E       *big.Int
			D       *big.Int
			Iqmp    *big.Int
			P       *big.Int
			Q       *big.Int
			Comment string
			Pad     []byte `ssh:"rest"`
		}
		if err := ssh.Unmarshal(pk.Rest, &rsa); err != nil {
			return nil, ErrNotOpenSSHKey
		}
		out.Comment = rsa.Comment
	case ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
		var ec struct {
			Curve   string
			Pub     []byte
			D       *big.Int
			Comment string
			Pad     []byte `ssh:"rest"`
		}
		if err := ssh.Unmarshal(pk.Rest, &ec); err != nil {
			return nil, ErrNotOpenSSHKey
		}
		out.Comment = ec.Comment
	}

	return out, nil
}
