package vaultops

import (
	"github.com/ollykeran/sshush/internal/vault"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// AddResult reports which key files were added, and the agent's key list before
// and after, so a front end can show the diff 'sshush add' shows.
type AddResult struct {
	Added  []string
	Before []*sshagent.Key
	After  []*sshagent.Key
}

// Add stores the private key files at paths in the vault, through the running
// vault agent. autoload controls whether each identity is loaded again after
// the daemon restarts. Paths must already be expanded and name real files:
// resolving a key by comment against the configured key_paths is the caller's
// job, since only it knows the config.
//
// Add stops at the first failure; keys added before it stay added.
func Add(env Env, paths []string, autoload bool) (AddResult, error) {
	c, err := open(env, "add", needAgent)
	if err != nil {
		return AddResult{}, err
	}
	defer c.close()

	res := AddResult{Before: c.agentKeys()}
	for _, path := range paths {
		if err := vault.AddPrivateKeyFile(c.session, path, autoload); err != nil {
			return res, describe(err, "add", path)
		}
		res.Added = append(res.Added, path)
	}
	res.After = c.agentKeys()
	return res, nil
}

// RemoveResult reports the fingerprints removed, and the agent's key list
// before and after.
type RemoveResult struct {
	Removed []string
	Before  []*sshagent.Key
	After   []*sshagent.Key
}

// Remove deletes the identities named by selectors from the vault. A selector
// is a SHA256 fingerprint, an exact comment, or a path to the private key file.
//
// Remove stops at the first failure; identities removed before it stay removed.
func Remove(env Env, selectors []string) (RemoveResult, error) {
	c, err := open(env, "remove", needStore|needAgent|mayUnlock)
	if err != nil {
		return RemoveResult{}, err
	}
	defer c.close()

	res := RemoveResult{Before: c.agentKeys()}
	for _, selector := range selectors {
		id, err := c.resolveIdentity(selector)
		if err != nil {
			return res, describe(err, "remove", selector)
		}
		pubKey, err := ssh.ParsePublicKey(id.PublicKey)
		if err != nil {
			return res, &OpError{
				Code: CodeVaultUnreadable,
				Msg:  "parse stored public key: " + err.Error(),
				Err:  err,
			}
		}
		if err := c.session.Remove(pubKey); err != nil {
			return res, describe(err, "remove", selector)
		}
		res.Removed = append(res.Removed, id.Fingerprint)
	}
	res.After = c.agentKeys()
	return res, nil
}
