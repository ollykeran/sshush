package vault

import (
	"crypto/ed25519"
	"crypto/x509"
	"errors"

	ssh "golang.org/x/crypto/ssh"
)

// marshalPrivateKey serializes a private key (as in agent.AddedKey) to bytes
// for storage. Ed25519 is stored as the seed; RSA, ECDSA and other PKCS#8
// key types are marshalled with x509.MarshalPKCS8PrivateKey.
func marshalPrivateKey(key interface{}) ([]byte, error) {
	switch k := key.(type) {
	case ed25519.PrivateKey:
		return k.Seed(), nil
	case *ed25519.PrivateKey:
		if k != nil {
			return k.Seed(), nil
		}
		return nil, errors.New("nil ed25519 private key")
	default:
		b, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, err
		}
		return b, nil
	}
}

// unmarshalPrivateKey deserializes bytes produced by marshalPrivateKey back
// into a key usable with ssh.NewSignerFromKey.
func unmarshalPrivateKey(seed []byte, keyType string) (interface{}, error) {
	if keyType == "ssh-ed25519" {
		if len(seed) != ed25519.SeedSize {
			return nil, errors.New("invalid ed25519 seed length")
		}
		return ed25519.NewKeyFromSeed(seed), nil
	}
	key, err := x509.ParsePKCS8PrivateKey(seed)
	if err != nil {
		return nil, err
	}
	return key, nil
}

// fingerprint returns a stable identifier for the public key (e.g. for vault identity key).
func fingerprint(pub ssh.PublicKey) string {
	return ssh.FingerprintSHA256(pub)
}
