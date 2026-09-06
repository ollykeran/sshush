package vaultops

import "github.com/ollykeran/sshush/internal/vault"

// LoadState reports whether an identity is loaded in the running agent.
// LoadUnknown means no agent answered, so the question has no answer.
type LoadState int

// The three answers to "is this identity loaded in the agent?".
const (
	LoadUnknown LoadState = iota
	LoadNo
	LoadYes
)

// String returns the one word both front ends show: "n/a", "no" or "yes".
func (l LoadState) String() string {
	switch l {
	case LoadYes:
		return "yes"
	case LoadNo:
		return "no"
	default:
		return "n/a"
	}
}

// IdentityView is one vault identity as the front ends display it.
type IdentityView struct {
	Fingerprint string
	Comment     string
	KeyType     string
	Filepath    string
	Autoload    bool
	Loaded      LoadState
}

// ListResult is the vault's contents plus the context needed to render them.
// Initialized false means no vault exists at VaultPath yet — the normal
// pre-init state, and not an error; each front end phrases its own remedy.
type ListResult struct {
	VaultPath      string
	Initialized    bool
	AgentReachable bool
	Identities     []IdentityView
}

// List returns every identity in the vault, marking which are loaded in the
// running agent. A missing agent is not an error: Loaded is LoadUnknown then.
//
// A vault holding no identities is answered without contacting the agent at
// all. There is nothing to ask about, and asking would prompt for a passphrase
// the user has no reason to give.
func List(env Env) (ListResult, error) {
	c, err := open(env, "list", needVaultPath)
	if err != nil {
		return ListResult{}, err
	}
	defer c.close()

	res := ListResult{VaultPath: c.vaultPath, Initialized: c.initialized}
	if !c.initialized {
		return res, nil
	}
	identities, err := vault.ListIdentities(c.store)
	if err != nil {
		return res, &OpError{Code: CodeVaultUnreadable, Msg: "list identities: " + err.Error(), Err: err}
	}
	if len(identities) == 0 {
		return res, nil
	}

	if err := c.attachAgent(agentIfRunning | mayUnlock); err != nil {
		return res, err
	}
	loaded, haveAgent := c.loadedFingerprints()
	res.AgentReachable = haveAgent

	res.Identities = make([]IdentityView, len(identities))
	for i, id := range identities {
		view := IdentityView{
			Fingerprint: id.Fingerprint,
			Comment:     id.Comment,
			KeyType:     id.KeyType,
			Filepath:    id.Filepath,
			Autoload:    id.Autoload,
			Loaded:      LoadUnknown,
		}
		if haveAgent {
			view.Loaded = LoadNo
			if _, ok := loaded[id.Fingerprint]; ok {
				view.Loaded = LoadYes
			}
		}
		res.Identities[i] = view
	}
	return res, nil
}
