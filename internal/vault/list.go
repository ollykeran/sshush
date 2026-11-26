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
	// Info describes the vault itself. HasInfo is false when the daemon is
	// older than this field and said nothing about it.
	Info    Info
	HasInfo bool
}

// Info is what a vault says about itself, apart from what it holds. It needs
// no passphrase: none of it is secret.
type Info struct {
	// CreatedAt is when the vault was created, as RFC 3339, or "" when the
	// vault predates the field.
	CreatedAt string
	// Recovery reports that a recovery phrase can unlock the vault.
	Recovery bool
	// FormatVersion is the version of the vault file's format.
	FormatVersion int
}

// Describe returns what store says about itself. An uninitialized store has
// nothing to say but its format version.
func Describe(store *VaultStore) Info {
	info := Info{FormatVersion: VaultFileVersion}
	if meta := store.GetMetadata(); meta != nil {
		info.CreatedAt = meta.CreatedAt
		info.Recovery = len(meta.RecoverySalt) > 0 && len(meta.WrappedMasterKey) > 0
	}
	return info
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

// wireInfo follows the identity records. It names itself, so a client can
// tell it from anything else a daemon might append, and skip what it does not
// recognise.
type wireInfo struct {
	Kind          string
	CreatedAt     string
	Recovery      bool
	FormatVersion uint32
	Rest          []byte `ssh:"rest"`
}

const wireInfoKind = "vault-info"

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
	return append(out, ssh.Marshal(wireInfo{
		Kind:          wireInfoKind,
		CreatedAt:     l.Info.CreatedAt,
		Recovery:      l.Info.Recovery,
		FormatVersion: uint32(l.Info.FormatVersion),
	})...)
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
	// What follows the records is optional: an older daemon sends nothing,
	// and a newer one may send something this version does not know.
	var info wireInfo
	if len(rest) > 0 && ssh.Unmarshal(rest, &info) == nil && info.Kind == wireInfoKind {
		l.Info = Info{CreatedAt: info.CreatedAt, Recovery: info.Recovery, FormatVersion: int(info.FormatVersion)}
		l.HasInfo = true
	}
	return l, nil
}
