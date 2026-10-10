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
	// needStore requires the vault file to hold an initialized vault, in the
	// cases where the file is what the verb works on: a vault the user named,
	// or a configured one with no vault agent to answer for it.
	needStore need = 1 << iota
	// needAgent requires a reachable, vault-backed agent.
	needAgent
	// agentIfRunning attaches an agent when one answers and leaves the session
	// nil otherwise, for verbs that can still report a partial answer.
	agentIfRunning
	// mayUnlock lets open prompt for a passphrase and unlock a locked agent,
	// when the front end can prompt and the agent serves the vault in question.
	mayUnlock
)

// conn is one unit of work: a single agent session, opened once and shared by
// every selector a verb handles, and the vault's contents from whichever of the
// agent and the vault file can supply them.
type conn struct {
	env       Env
	verb      string
	vaultPath string
	session   *agent.Session
	backend   agent.Backend
	dialErr   error // why there is no session, when the agent was optional

	// The vault file is opened only when something has to be read from it, so
	// a config whose vault_path cannot be read from here costs nothing while
	// the agent answers.
	store       *vault.VaultStore
	initialized bool

	contents     roster
	haveContents bool
}

// roster is what a vault holds, from the agent serving it or from its file.
type roster struct {
	// vaultPath is the vault file. When the agent answered it is that agent's
	// own name for it, which may mean nothing on this machine.
	vaultPath   string
	initialized bool
	identities  []vault.IdentityInfo
}

// open satisfies n for verb and returns a conn the caller must close.
func open(env Env, verb string, n need) (*conn, error) {
	c := &conn{env: env, verb: verb, vaultPath: env.vaultFile()}

	// A named vault is checked before the agent, so a mistyped --vault-path is
	// reported as a missing vault rather than as whatever the agent makes of it.
	if n&needStore != 0 && env.namedVault() {
		if err := c.requireStore(); err != nil {
			return nil, err
		}
	}
	if err := c.attachAgent(n); err != nil {
		// With no vault agent the configured file is all there is, and "there is
		// no vault there yet" explains the failure better than the agent does.
		if n&needStore != 0 && c.vaultPath != "" {
			if storeErr := c.requireStore(); storeErr != nil {
				return nil, storeErr
			}
		}
		return nil, c.env.remedy(err)
	}
	return c, nil
}

// attachAgent opens the agent session n asks for.
func (c *conn) attachAgent(n need) error {
	if n&(needAgent|agentIfRunning) == 0 {
		return nil
	}
	session, err := agent.Open(c.env.SocketPath)
	if err != nil {
		if n&needAgent != 0 {
			return noAgentError(c.verb, err)
		}
		c.dialErr = err
		return nil
	}
	backend, berr := session.Backend()
	if berr == nil {
		c.backend = backend
	} else if _, listErr := session.List(); listErr != nil {
		// It took the connection and then answered nothing at all, which is
		// what a relay does when the agent behind it is gone. That is no
		// agent, not the wrong kind of agent.
		_ = session.Close()
		if n&needAgent != 0 {
			return noAgentError(c.verb, berr)
		}
		c.dialErr = berr
		return nil
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

// RequireVaultAgent reports whether verb could run right now: an agent is
// reachable and vault-backed. Front ends call it to fail before asking the user
// for something long and secret, the way [InitTarget] does for init. It opens
// and closes its own session rather than holding one across an interactive
// prompt, so the verb that follows dials again.
func RequireVaultAgent(env Env, verb string) error {
	c, err := open(env, verb, needAgent)
	if err != nil {
		return err
	}
	c.close()
	return nil
}

// close releases the agent session, if any.
func (c *conn) close() {
	if c != nil && c.session != nil {
		_ = c.session.Close()
	}
}

// describe phrases err for this verb, as the package-level describe does, with
// the remedy adjusted for an agent this sshush does not manage.
func (c *conn) describe(err error, selector string) error {
	return c.env.remedy(describe(err, c.verb, selector))
}

// openStore reads the vault file, once. It is not an error for the file to be
// missing; initialized says whether a vault was found in it.
func (c *conn) openStore() error {
	if c.store != nil {
		return nil
	}
	store, err := vault.Open(c.vaultPath)
	if err != nil {
		return &OpError{
			Code: CodeVaultUnreadable,
			Msg:  "open vault: " + err.Error(),
			Err:  err,
		}
	}
	c.store = store
	c.initialized = store.GetMetadata() != nil
	return nil
}

// requireStore fails unless the vault file holds an initialized vault.
func (c *conn) requireStore() error {
	if err := c.openStore(); err != nil {
		return err
	}
	if !c.initialized {
		return notInitializedError(c.vaultPath)
	}
	return nil
}

// noAgentError says nothing answered on the agent socket verb needed.
func noAgentError(verb string, cause error) *OpError {
	return &OpError{
		Code: CodeNoAgent,
		Msg:  "vault " + verb + " requires a running vault agent",
		Hint: "Start it with 'sshush start'.",
		Err:  cause,
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

// remedy rewrites the hints that tell the user to start or reload the daemon,
// for an agent those commands do not reach. 'sshush start' and 'sshush reload'
// refuse for an agent this sshush does not manage, and reload restarts only the
// relay for one on Windows, so pointing at them sends the user in a circle.
func (e Env) remedy(err error) error {
	var op *OpError
	if !errors.As(err, &op) || op.Hint == "" {
		return err
	}
	elsewhere := e.AgentUnmanaged || e.AgentElsewhere
	switch {
	case op.Code == CodeNoAgent && e.AgentUnmanaged:
		op.Hint = "Nothing answers on " + utils.DisplayPath(e.SocketPath) + ". This sshush does not manage that agent ([agent].type = \"external\"): start it, or the relay that reaches it, where it runs."
	case op.Code == CodeNotVaultAgent && elsewhere:
		op.Hint = "this agent does not speak sshush's protocol; if it is an sshushd from an older install, restart it where it runs."
	case op.Code == CodeAgentTooOld && elsewhere:
		op.Hint = "It is an sshushd older than this sshush: restart it where it runs, or set [vault].vault_path to read the vault file instead."
	}
	return err
}

// ownVault reports whether this verb works on the vault the attached agent
// serves, so the agent is the one to ask what is in it.
func (c *conn) ownVault() bool {
	return c.session != nil && c.backend.Mode == "vault" && !c.env.namedVault()
}

// unlockIfLocked prompts for a passphrase and unlocks a locked agent, but only
// when the front end can prompt and the agent serves the vault this verb works
// on, and reports whether the agent ended up unlocked by it. Failures are
// deliberately swallowed: the verb's own error names the problem better than
// "unlock failed" would.
func (c *conn) unlockIfLocked() bool {
	if c.session == nil || c.env.AskPassphrase == nil {
		return false
	}
	if c.backend.Mode != "vault" || !c.backend.VaultLocked {
		return false
	}
	if c.env.namedVault() {
		// The user named a file. Only config can say whether the agent serves
		// it, and if it does not, this is not the moment to ask for the
		// agent's passphrase.
		if agentVault := c.env.agentVaultFile(); agentVault == "" || c.vaultPath != agentVault {
			return false
		}
	}
	passphrase, err := c.env.AskPassphrase("Passphrase: ")
	if err != nil {
		return false
	}
	defer zero(passphrase)
	if err := c.session.Unlock(passphrase); err != nil {
		return false
	}
	c.backend.VaultLocked = false
	return true
}

// roster returns what the vault this verb works on holds, reading it once.
//
// The agent is asked first when the vault is its own. It can refuse — a locked
// vault tells whoever reaches its socket nothing, and an older sshushd does not
// know the question — and then the vault file answers instead, if this machine
// has one.
func (c *conn) roster() (roster, error) {
	if c.haveContents {
		return c.contents, nil
	}
	r, err := c.readRoster()
	if err != nil {
		return roster{}, err
	}
	c.contents, c.haveContents = r, true
	return r, nil
}

func (c *conn) readRoster() (roster, error) {
	var refused error
	if c.ownVault() {
		listing, err := vault.Identities(c.session)
		if errors.Is(err, agent.ErrVaultLocked) && c.vaultPath == "" && c.unlockIfLocked() {
			// Only the agent can say what this vault holds, which makes the
			// question worth a passphrase. With a vault file to read instead,
			// the caller decides whether what it finds there is.
			listing, err = vault.Identities(c.session)
		}
		switch {
		case err == nil:
			return roster{vaultPath: listing.VaultPath, initialized: true, identities: listing.Identities}, nil
		case errors.Is(err, agent.ErrVaultLocked), errors.Is(err, agent.ErrOpUnknown):
			refused = err
		default:
			return roster{}, c.describe(err, "")
		}
	}

	if c.vaultPath == "" {
		switch {
		case refused == nil && c.dialErr != nil && c.env.AgentUnmanaged:
			// Someone else's agent is the only place this config keeps a vault,
			// so its absence is the problem, not a missing vault_path.
			return roster{}, c.env.remedy(noAgentError(c.verb, c.dialErr))
		case refused == nil:
			return roster{}, &OpError{
				Code: CodeNoVaultPath,
				Msg:  "vault path required",
				Hint: "Set [vault].vault_path in config, or pass --vault-path.",
			}
		case errors.Is(refused, agent.ErrOpUnknown):
			return roster{}, c.env.remedy(&OpError{
				Code: CodeAgentTooOld,
				Msg:  "this vault agent cannot say what its vault holds",
				Hint: "It is an sshushd older than this sshush: restart it with 'sshush reload', or set [vault].vault_path to read the vault file instead.",
				Err:  refused,
			})
		default:
			return roster{}, c.describe(refused, "")
		}
	}

	if err := c.openStore(); err != nil {
		return roster{}, err
	}
	r := roster{vaultPath: c.vaultPath, initialized: c.initialized}
	if !c.initialized {
		return r, nil
	}
	identities, err := vault.ListIdentities(c.store)
	if err != nil {
		return roster{}, &OpError{Code: CodeVaultUnreadable, Msg: "list identities: " + err.Error(), Err: err}
	}
	r.identities = identities
	return r, nil
}

// resolveIdentity turns a user-typed selector into a vault identity. It accepts
// a SHA256 fingerprint, an exact comment, or a path to the private key file.
func (c *conn) resolveIdentity(selector string) (vault.IdentityInfo, error) {
	r, err := c.roster()
	if err != nil {
		return vault.IdentityInfo{}, err
	}
	if !r.initialized {
		return vault.IdentityInfo{}, notInitializedError(r.vaultPath)
	}
	path := utils.ExpandHomeDirectory(selector)
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		pubKey, _, _, err := agent.ParseKeyFromPath(path)
		if err == nil {
			return vault.ResolveIdentityByFingerprint(r.identities, ssh.FingerprintSHA256(pubKey))
		}
		if errors.Is(err, openssh.ErrEncryptedPrivateKey) {
			return vault.IdentityInfo{}, err
		}
	}
	return vault.ResolveIdentity(r.identities, selector)
}

// resolveFingerprint is resolveIdentity reduced to a fingerprint. A SHA256
// selector for the agent's own vault passes straight through for the agent to
// look up: the TUI selects a table row and already holds the fingerprint, and
// an agent too old to list its vault can still act on one. A vault the user
// named is always consulted, so naming one that is not there is still an error.
func (c *conn) resolveFingerprint(selector string) (string, error) {
	if strings.HasPrefix(selector, "SHA256:") && !c.env.namedVault() {
		return selector, nil
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
