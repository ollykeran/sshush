package keys

import (
	ssh "golang.org/x/crypto/ssh"
)

// PublicKeyFromPEM parses a private key PEM blob and returns its public key and raw key.
func PublicKeyFromPEM(privPEM []byte) (ssh.PublicKey, interface{}, error) {
	key, err := ssh.ParseRawPrivateKey(privPEM)
	if err != nil {
		return nil, nil, err
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, nil, err
	}
	return signer.PublicKey(), key, nil
}
