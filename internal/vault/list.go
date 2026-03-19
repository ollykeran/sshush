package vault

import (
	ssh "golang.org/x/crypto/ssh"
)

// IdentityInfo holds metadata for one vault identity (no private key material).
type IdentityInfo struct {
	Fingerprint string
	Comment     string
	KeyType     string
	Autoload    bool
}

// ListIdentities returns all identities in the vault with metadata.
// Key type is parsed from public_key; no unlock or master key needed.
func ListIdentities(store *VaultStore) ([]IdentityInfo, error) {
	identities := store.AllIdentities()
	out := make([]IdentityInfo, 0, len(identities))
	for _, id := range identities {
		keyType := "?"
		if pub, err := ssh.ParsePublicKey(id.PublicKey); err == nil {
			keyType = pub.Type()
		}
		out = append(out, IdentityInfo{
			Fingerprint: id.Fingerprint,
			Comment:     id.Comment,
			KeyType:     keyType,
			Autoload:    id.Autoload,
		})
	}
	return out, nil
}
