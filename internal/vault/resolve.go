package vault

import "errors"

var (
	// ErrIdentityNotFound means no vault row matched the selector.
	ErrIdentityNotFound = errors.New("vault: no identity matches")
	// ErrAmbiguousComment means more than one identity shares the comment.
	ErrAmbiguousComment = errors.New("vault: multiple identities match that comment")
)

// ResolveIdentity finds an identity by exact SHA256 fingerprint string or by
// exact comment. ids is a vault's contents from either source: [ListIdentities]
// for a vault file, [Identities] for the vault behind an agent.
func ResolveIdentity(ids []IdentityInfo, spec string) (IdentityInfo, error) {
	if spec == "" {
		return IdentityInfo{}, ErrIdentityNotFound
	}
	for _, id := range ids {
		if id.Fingerprint == spec {
			return id, nil
		}
	}
	var matches []IdentityInfo
	for _, id := range ids {
		if id.Comment == spec {
			matches = append(matches, id)
		}
	}
	switch len(matches) {
	case 0:
		return IdentityInfo{}, ErrIdentityNotFound
	case 1:
		return matches[0], nil
	default:
		return IdentityInfo{}, ErrAmbiguousComment
	}
}

// ResolveIdentityByFingerprint finds an identity by fingerprint only.
func ResolveIdentityByFingerprint(ids []IdentityInfo, fp string) (IdentityInfo, error) {
	for _, id := range ids {
		if id.Fingerprint == fp {
			return id, nil
		}
	}
	return IdentityInfo{}, ErrIdentityNotFound
}
