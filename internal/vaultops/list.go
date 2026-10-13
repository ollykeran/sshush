package vaultops

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
//
// VaultPath is the vault file. When the running agent answered for its own
// vault it is that agent's name for the file, which need not exist on this
// machine: a daemon on Windows reached from WSL reports a Windows path.
type ListResult struct {
	VaultPath      string
	Initialized    bool
	AgentReachable bool
	Identities     []IdentityView
}

// List returns every identity in the vault, marking which are loaded in the
// running agent. The running vault agent is asked what its vault holds; the
// vault file is read when there is no such agent, when it will not say, or
// when the user named a file. A missing agent is not an error: Loaded is
// LoadUnknown then.
//
// A vault found to hold no identities is answered without asking for a
// passphrase. There is nothing to report as loaded, so the user has no reason
// to give one.
func List(env Env) (ListResult, error) {
	c, err := open(env, "list", agentIfRunning)
	if err != nil {
		return ListResult{}, err
	}
	defer c.close()

	r, err := c.roster()
	if err != nil {
		return ListResult{}, err
	}
	res := ListResult{VaultPath: r.vaultPath, Initialized: r.initialized}
	if len(r.identities) == 0 {
		return res, nil
	}

	c.unlockIfLocked()
	loaded, haveAgent := c.loadedFingerprints()
	res.AgentReachable = haveAgent

	res.Identities = make([]IdentityView, len(r.identities))
	for i, id := range r.identities {
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
