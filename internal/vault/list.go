package vault

import (
	"fmt"

	ssh "golang.org/x/crypto/ssh"
)

// IdentityInfo holds metadata for one vault identity (no private key material).
type IdentityInfo struct {
	Fingerprint string
	Comment     string
	KeyType     string
	Autoload    bool
	Filepath    string
	PublicKey   []byte // wire-format public key, as an agent lists it
}

// ListIdentities returns all identities in the vault with metadata.
// Key type is parsed from public_key; no unlock or master key needed.
func ListIdentities(store *VaultStore) ([]IdentityInfo, error) {
	identities := store.AllIdentities()
	out := make([]IdentityInfo, 0, len(identities))
	for _, id := range identities {
		out = append(out, IdentityInfo{
			Fingerprint: id.Fingerprint,
			Comment:     id.Comment,
			KeyType:     keyTypeOf(id.PublicKey),
			Autoload:    id.Autoload,
			Filepath:    id.Filepath,
			PublicKey:   id.PublicKey,
		})
	}
	return out, nil
}

// keyTypeOf names the algorithm of a wire-format public key, or "?" for a blob
// that does not parse.
func keyTypeOf(publicKey []byte) string {
	if pub, err := ssh.ParsePublicKey(publicKey); err == nil {
		return pub.Type()
	}
	return "?"
}

// Listing is a vault's identities as the agent serving it reports them.
type Listing struct {
	// VaultPath is the vault file as the agent's own host names it. A client
	// reaching the agent from somewhere else — WSL talking to a daemon on
	// Windows — cannot open it, so it is for display only.
	VaultPath  string
	Identities []IdentityInfo
}

// The listing travels as SSH wire data. Each identity is its own length-prefixed
// record, and both the listing and a record may be followed by fields this
// version does not know, so a newer daemon can say more without breaking an
// older client.

type wireListing struct {
	VaultPath string
	Count     uint32
	Rest      []byte `ssh:"rest"`
}

type wireRecord struct {
	Record []byte
	Rest   []byte `ssh:"rest"`
}

type wireIdentity struct {
	Fingerprint string
	PublicKey   []byte
	Comment     string
	Filepath    string
	Autoload    bool
	Rest        []byte `ssh:"rest"`
}

// encodeListing builds the list-identities response data.
func encodeListing(l Listing) []byte {
	out := ssh.Marshal(wireListing{VaultPath: l.VaultPath, Count: uint32(len(l.Identities))})
	for _, id := range l.Identities {
		record := ssh.Marshal(wireIdentity{
			Fingerprint: id.Fingerprint,
			PublicKey:   id.PublicKey,
			Comment:     id.Comment,
			Filepath:    id.Filepath,
			Autoload:    id.Autoload,
		})
		out = append(out, ssh.Marshal(wireRecord{Record: record})...)
	}
	return out
}

// decodeListing parses the list-identities response data.
func decodeListing(data []byte) (Listing, error) {
	var head wireListing
	if err := ssh.Unmarshal(data, &head); err != nil {
		return Listing{}, fmt.Errorf("vault: list-identities: parse header: %w", err)
	}
	l := Listing{VaultPath: head.VaultPath}
	rest := head.Rest
	for i := uint32(0); i < head.Count; i++ {
		var rec wireRecord
		if err := ssh.Unmarshal(rest, &rec); err != nil {
			return Listing{}, fmt.Errorf("vault: list-identities: parse record %d of %d: %w", i+1, head.Count, err)
		}
		rest = rec.Rest
		var id wireIdentity
		if err := ssh.Unmarshal(rec.Record, &id); err != nil {
			return Listing{}, fmt.Errorf("vault: list-identities: parse identity %d of %d: %w", i+1, head.Count, err)
		}
		l.Identities = append(l.Identities, IdentityInfo{
			Fingerprint: id.Fingerprint,
			Comment:     id.Comment,
			KeyType:     keyTypeOf(id.PublicKey),
			Autoload:    id.Autoload,
			Filepath:    id.Filepath,
			PublicKey:   id.PublicKey,
		})
	}
	return l, nil
}
