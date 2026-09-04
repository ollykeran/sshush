package vaultops

import "github.com/ollykeran/sshush/internal/vault"

// LoadResult names the fingerprints made visible in the running agent.
type LoadResult struct {
	Loaded []string
}

// SessionLoad makes non-autoload identities visible in the running agent until
// that agent restarts, so ssh can use them without the PEM file. A selector is
// a SHA256 fingerprint, or — when a vault path is configured — an exact comment
// or a path to the private key file.
//
// SessionLoad stops at the first failure; identities loaded before it stay
// loaded.
func SessionLoad(env Env, selectors []string) (LoadResult, error) {
	c, err := open(env, "load", needAgent|mayUnlock)
	if err != nil {
		return LoadResult{}, err
	}
	defer c.close()

	var res LoadResult
	for _, selector := range selectors {
		fingerprint, err := c.resolveFingerprint(selector)
		if err != nil {
			return res, describe(err, "load", selector)
		}
		if err := vault.SessionLoad(c.session, fingerprint); err != nil {
			return res, describe(err, "load", selector)
		}
		res.Loaded = append(res.Loaded, fingerprint)
	}
	return res, nil
}

// AutoloadResult names the fingerprints whose autoload flag changed, and the
// state they were set to.
type AutoloadResult struct {
	Changed []string
	On      bool
}

// SetAutoload persists whether each selected identity is loaded automatically
// after the daemon restarts. Selectors resolve as they do for [SessionLoad].
//
// SetAutoload stops at the first failure; identities changed before it keep
// their new setting.
func SetAutoload(env Env, selectors []string, on bool) (AutoloadResult, error) {
	c, err := open(env, "autoload", needAgent|mayUnlock)
	if err != nil {
		return AutoloadResult{}, err
	}
	defer c.close()

	res := AutoloadResult{On: on}
	for _, selector := range selectors {
		fingerprint, err := c.resolveFingerprint(selector)
		if err != nil {
			return res, describe(err, "autoload", selector)
		}
		if err := vault.SetAutoload(c.session, fingerprint, on); err != nil {
			return res, describe(err, "autoload", selector)
		}
		res.Changed = append(res.Changed, fingerprint)
	}
	return res, nil
}
