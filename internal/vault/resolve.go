package vault

import "errors"

var (
	// ErrIdentityNotFound means no vault row matched the selector.
	ErrIdentityNotFound = errors.New("vault: no identity matches")
	// ErrAmbiguousComment means more than one identity shares the comment.
	ErrAmbiguousComment = errors.New("vault: multiple identities match that comment")
)

// ResolveIdentity finds an identity by exact SHA256 fingerprint string or by exact comment.
func ResolveIdentity(store *VaultStore, spec string) (Identity, error) {
	if spec == "" {
		return Identity{}, ErrIdentityNotFound
	}
	ids := store.AllIdentities()
	for _, id := range ids {
		if id.Fingerprint == spec {
			return id, nil
		}
	}
	var matches []Identity
	for _, id := range ids {
		if id.Comment == spec {
			matches = append(matches, id)
		}
	}
	switch len(matches) {
	case 0:
		return Identity{}, ErrIdentityNotFound
	case 1:
		return matches[0], nil
	default:
		return Identity{}, ErrAmbiguousComment
	}
}

// ResolveIdentityByFingerprint finds an identity by fingerprint only.
func ResolveIdentityByFingerprint(store *VaultStore, fp string) (Identity, error) {
	for _, id := range store.AllIdentities() {
		if id.Fingerprint == fp {
			return id, nil
		}
	}
	return Identity{}, ErrIdentityNotFound
}
