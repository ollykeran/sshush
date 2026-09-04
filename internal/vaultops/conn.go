package vaultops

import (
	"errors"
	"os"
	"strings"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/openssh"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// need is the set of preconditions open must satisfy before a verb runs.
type need uint8

const (
	// needVaultPath requires Env.VaultPath to name something.
	needVaultPath need = 1 << iota
	// needStore requires an initialized vault at that path. Implies needVaultPath.
	needStore
	// needAgent requires a reachable, vault-backed agent.
	needAgent
	// agentIfRunning attaches an agent when one answers and leaves the session
	// nil otherwise, for verbs that can still report a partial answer.
	agentIfRunning
	// mayUnlock lets open prompt for a passphrase and unlock a locked agent,
	// when the front end can prompt and the agent serves this very vault.
	mayUnlock
)

// conn is one unit of work: the resolved vault path, the store behind it, and a
// single agent session, opened once and shared by every selector a verb
// handles. The store is opened whenever a vault path is configured; need only
// decides whether a missing or uninitialized one is fatal.
type conn struct {
	env         Env
	verb        string
	vaultPath   string
	store       *vault.VaultStore
	initialized bool
	session     *agent.Session
	backend     agent.Backend
}

// open satisfies n for verb and returns a conn the caller must close.
func open(env Env, verb string, n need) (*conn, error) {
	c := &conn{env: env, verb: verb, vaultPath: env.vaultFile()}

	if n&(needVaultPath|needStore) != 0 && c.vaultPath == "" {
		return nil, &OpError{
			Code: CodeNoVaultPath,
			Msg:  "vault path required",
			Hint: "Set [vault].vault_path in config, or pass --vault-path.",
		}
	}
	if c.vaultPath != "" {
		store, err := vault.Open(c.vaultPath)
		if err != nil {
			return nil, &OpError{
				Code: CodeVaultUnreadable,
				Msg:  "open vault: " + err.Error(),
				Err:  err,
			}
		}
		c.store = store
		c.initialized = store.GetMetadata() != nil
	}
	if n&needStore != 0 && !c.initialized {
		return nil, notInitializedError(c.vaultPath)
	}

	if err := c.attachAgent(n); err != nil {
		return nil, err
	}
	return c, nil
}

// attachAgent opens the agent session n asks for. It is separate from open so a
// verb can inspect the store first and skip the agent entirely — an empty vault
// has nothing to ask an agent about, and asking would start prompting for a
// passphrase nobody needs to give.
func (c *conn) attachAgent(n need) error {
	if n&(needAgent|agentIfRunning) == 0 {
		return nil
	}
	session, err := agent.Open(c.env.SocketPath)
	if err != nil {
		if n&needAgent != 0 {
			return &OpError{
				Code: CodeNoAgent,
				Msg:  "vault " + c.verb + " requires a running vault agent",
				Hint: "Start it with 'sshush start'.",
				Err:  err,
			}
		}
		return nil
	}
	backend, berr := session.Backend()
	if berr == nil {
		c.backend = backend
	}
	if n&needAgent != 0 && (berr != nil || backend.Mode != "vault") {
		_ = session.Close()
		return gateError(c.verb, c.backend, berr)
	}
	c.session = session
	if n&mayUnlock != 0 {
		c.unlockIfLocked()
	}
	return nil
}

// close releases the agent session, if any.
func (c *conn) close() {
	if c != nil && c.session != nil {
		_ = c.session.Close()
	}
}

// notInitializedError names the vault path the caller asked for, so a front end
// can offer the right way to create it.
func notInitializedError(vaultPath string) *OpError {
	return &OpError{
		Code: CodeVaultNotInitialized,
		Msg:  "vault not found or not initialized at " + utils.DisplayPath(vaultPath),
	}
}

// gateError explains why verb cannot run without a vault agent. An agent that
// answers but does not speak sshush's protocol is almost always an sshushd left
// running from an older install, which a restart fixes, so say that.
func gateError(verb string, backend agent.Backend, cause error) *OpError {
	e := &OpError{
		Code: CodeNotVaultAgent,
		Msg:  "vault " + verb + " requires a running vault agent",
		Err:  cause,
	}
	if !backend.SpeaksOps {
		e.Hint = "this agent does not speak sshush's protocol; if you upgraded sshush while the daemon was running, restart it with 'sshush reload'."
	}
	return e
}

// unlockIfLocked prompts for a passphrase and unlocks a locked agent, but only
// when the front end can prompt and the agent serves this very vault. Failures
// are deliberately swallowed: the verb's own error names the problem better
// than "unlock failed" would.
func (c *conn) unlockIfLocked() {
	if c.session == nil || c.env.AskPassphrase == nil {
		return
	}
	agentVault := c.env.agentVaultFile()
	if agentVault == "" || c.vaultPath == "" || c.vaultPath != agentVault {
		return
	}
	if c.backend.Mode != "vault" || !c.backend.VaultLocked {
		return
	}
	passphrase, err := c.env.AskPassphrase("Passphrase: ")
	if err != nil {
		return
	}
	defer zero(passphrase)
	if err := c.session.Unlock(passphrase); err == nil {
		c.backend.VaultLocked = false
	}
}

// resolveIdentity turns a user-typed selector into a stored vault identity. It
// accepts a SHA256 fingerprint, an exact comment, or a path to the private key
// file, and needs an initialized store.
func (c *conn) resolveIdentity(selector string) (vault.Identity, error) {
	if !c.initialized {
		return vault.Identity{}, notInitializedError(c.vaultPath)
	}
	path := utils.ExpandHomeDirectory(selector)
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		pubKey, _, _, err := agent.ParseKeyFromPath(path)
		if err == nil {
			return vault.ResolveIdentityByFingerprint(c.store, ssh.FingerprintSHA256(pubKey))
		}
		if errors.Is(err, openssh.ErrEncryptedPrivateKey) {
			return vault.Identity{}, err
		}
	}
	return vault.ResolveIdentity(c.store, selector)
}

// resolveFingerprint is resolveIdentity reduced to a fingerprint. With no vault
// path configured at all — the TUI, which selects a table row and already holds
// the fingerprint — a SHA256 selector passes straight through for the agent to
// look up. A vault path that was configured but holds no vault is still an
// error: the caller asked for that vault by name.
func (c *conn) resolveFingerprint(selector string) (string, error) {
	if c.store == nil {
		if strings.HasPrefix(selector, "SHA256:") {
			return selector, nil
		}
		return "", &OpError{
			Code: CodeNoVaultPath,
			Msg:  "vault path required to resolve " + selector,
			Hint: "Set [vault].vault_path in config, or select by SHA256 fingerprint.",
		}
	}
	id, err := c.resolveIdentity(selector)
	if err != nil {
		return "", err
	}
	return id.Fingerprint, nil
}

// agentKeys returns the agent's current key list, or nil when there is no agent
// or it refuses. Callers use it for the before-and-after snapshots they diff,
// which is display only, so a refusal is not worth failing the verb over.
func (c *conn) agentKeys() []*sshagent.Key {
	if c.session == nil {
		return nil
	}
	keys, err := c.session.List()
	if err != nil {
		return nil
	}
	return keys
}

// loadedFingerprints is the set of fingerprints the agent currently serves. The
// second result is false when there is no agent to ask, which is a different
// answer from an agent serving nothing.
func (c *conn) loadedFingerprints() (map[string]struct{}, bool) {
	if c.session == nil {
		return nil, false
	}
	keys, err := c.session.List()
	if err != nil {
		return nil, false
	}
	set := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if pub, err := ssh.ParsePublicKey(k.Blob); err == nil {
			set[ssh.FingerprintSHA256(pub)] = struct{}{}
		}
	}
	return set, true
}
