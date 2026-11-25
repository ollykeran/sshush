package vaultops

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/secfile"
	"github.com/ollykeran/sshush/internal/vault"
)

// Changing the passphrase re-encrypts the whole vault, and the recovery phrase
// wraps the key it is encrypted under, so a change of passphrase comes with a
// new recovery phrase too. Both go through a running vault agent when there is
// one, since it holds the vault in memory and would write it back over a change
// made to the file; with no agent the file is changed directly.

// SecretResult reports a change of the vault's secrets. Mnemonic is the new
// recovery phrase, or "" when the vault no longer has one: show it once, and
// neither log nor store it.
type SecretResult struct {
	Mnemonic string
	// RecoveryFile is recovery.txt beside the vault, rewritten with the new
	// phrase, or removed when there is none; "" when there is no such file
	// on this machine to keep current.
	RecoveryFile string
}

// CheckNewPassphrase reports whether pass would be accepted as a new
// passphrase, so a front end can say so before asking for it again.
func CheckNewPassphrase(pass []byte) error {
	if err := vault.DefaultPassphrasePolicy.ValidateNew(pass); err != nil {
		return &OpError{Code: CodeWeakPassphrase, Msg: err.Error(), Err: err}
	}
	return nil
}

// ChangePassphrase makes newPass the vault's passphrase, proving the right to
// with cred (its passphrase, or its recovery phrase). With recovery set, a new
// recovery phrase is generated and returned; without, the vault is left with
// none. An unlocked agent stays unlocked.
func ChangePassphrase(env Env, cred vault.Credential, newPass []byte, recovery bool) (SecretResult, error) {
	if err := CheckNewPassphrase(newPass); err != nil {
		return SecretResult{}, err
	}
	var res SecretResult
	if recovery {
		m, err := vault.GenerateRecoveryMnemonic()
		if err != nil {
			return res, &OpError{Code: CodeLocalIO, Msg: "generate recovery phrase: " + err.Error(), Err: err}
		}
		res.Mnemonic = m
	}
	err := changeSecrets(env, "passwd", cred,
		func(s *agent.Session) error { return vault.ChangePassphraseOp(s, cred, newPass, res.Mnemonic) },
		func(store *vault.VaultStore) error {
			key, err := vault.ChangePassphrase(store, cred, newPass, res.Mnemonic)
			clear(key)
			return err
		})
	if err != nil {
		return SecretResult{}, err
	}
	res.RecoveryFile = keepRecoveryFile(env, res.Mnemonic)
	return res, nil
}

// RegenerateRecovery gives the vault a new recovery phrase, proving the right
// to with cred; the old phrase stops working. The passphrase is unchanged.
func RegenerateRecovery(env Env, cred vault.Credential) (SecretResult, error) {
	m, err := vault.GenerateRecoveryMnemonic()
	if err != nil {
		return SecretResult{}, &OpError{Code: CodeLocalIO, Msg: "generate recovery phrase: " + err.Error(), Err: err}
	}
	err = changeSecrets(env, "recovery", cred,
		func(s *agent.Session) error { return vault.SetRecoveryOp(s, cred, m) },
		func(store *vault.VaultStore) error { return vault.SetRecovery(store, cred, m) })
	if err != nil {
		return SecretResult{}, err
	}
	return SecretResult{Mnemonic: m, RecoveryFile: keepRecoveryFile(env, m)}, nil
}

// changeSecrets runs viaAgent when a vault agent serves the vault in question,
// else inFile on the vault file.
func changeSecrets(env Env, verb string, cred vault.Credential, viaAgent func(*agent.Session) error, inFile func(*vault.VaultStore) error) error {
	c, err := open(env, verb, agentIfRunning)
	if err != nil {
		return err
	}
	defer c.close()
	served := c.ownVault()
	if !served && c.session != nil && c.backend.Mode == "vault" {
		// A named file the agent serves is still the agent's to change.
		served = c.env.agentVaultFile() != "" && samePath(c.env.agentVaultFile(), c.vaultPath)
	}
	if served {
		err := viaAgent(c.session)
		if errors.Is(err, agent.ErrOpUnknown) {
			return c.env.remedy(&OpError{
				Code: CodeAgentTooOld,
				Msg:  "the running sshushd is older than this sshush and cannot change the vault's secrets",
				Hint: "Restart it with 'sshush reload', then try again.",
				Err:  err,
			})
		}
		return secretError(c, err, cred)
	}
	// With no agent, or one serving some other vault, the file is this
	// verb's to change: nothing holds it in memory.
	if c.vaultPath == "" {
		return &OpError{
			Code: CodeNoVaultPath,
			Msg:  "no vault agent is running and no vault file is configured",
			Hint: "Start the agent with 'sshush start', or pass --vault-path.",
		}
	}
	if err := c.requireStore(); err != nil {
		return err
	}
	return secretError(c, inFile(c.store), cred)
}

// secretError phrases a failure to change the vault's secrets.
func secretError(c *conn, err error, cred vault.Credential) error {
	if err == nil {
		return nil
	}
	what := "passphrase"
	if cred.Mnemonic != "" {
		what = "recovery phrase"
	}
	switch {
	case errors.Is(err, agent.ErrWrongPassphrase), errors.Is(err, vault.ErrWrongPassphrase):
		return &OpError{Code: CodeWrongPassphrase, Msg: "wrong " + what, Err: err}
	case errors.Is(err, agent.ErrNoRecovery), errors.Is(err, vault.ErrNoRecoveryEnabled):
		return &OpError{Code: CodeNoRecovery, Msg: "this vault has no recovery phrase to prove it with", Err: err}
	case errors.Is(err, vault.ErrWeakPassphrase):
		return &OpError{Code: CodeWeakPassphrase, Msg: err.Error(), Err: err}
	}
	return c.describe(err, "")
}

// keepRecoveryFile keeps recovery.txt beside the vault in step with the vault:
// the new phrase, or no file when there is no phrase. It only touches a file
// that is already there, beside a vault file on this machine, and returns its
// path, or "".
func keepRecoveryFile(env Env, mnemonic string) string {
	vaultFile := env.agentVaultFile()
	if vaultFile == "" {
		vaultFile = env.vaultFile()
	}
	if vaultFile == "" {
		return ""
	}
	path := filepath.Join(filepath.Dir(vaultFile), "recovery.txt")
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	if mnemonic == "" {
		if os.Remove(path) != nil {
			return ""
		}
		return path
	}
	if secfile.WriteFile(path, []byte(mnemonic+"\n")) != nil {
		return ""
	}
	return path
}
