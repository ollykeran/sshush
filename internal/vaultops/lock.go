package vaultops

import "github.com/ollykeran/sshush/internal/vault"

// Lock wipes the vault agent's in-memory master key. Locking needs no
// passphrase: there is nothing to prove, only something to forget.
func Lock(env Env) error {
	c, err := open(env, "lock", needAgent)
	if err != nil {
		return err
	}
	defer c.close()
	return describe(c.session.Lock(nil), "lock", "")
}

// UnlockPassphrase unlocks the vault agent with its master passphrase. The
// caller owns passphrase and should zero it; UnlockPassphrase does not.
func UnlockPassphrase(env Env, passphrase []byte) error {
	c, err := open(env, "unlock", needAgent)
	if err != nil {
		return err
	}
	defer c.close()
	return describe(c.session.Unlock(passphrase), "unlock", "")
}

// UnlockRecovery unlocks the vault agent with its 24-word BIP-39 recovery
// phrase. A vault created without recovery reports [CodeNoRecovery] rather than
// a wrong-phrase error.
func UnlockRecovery(env Env, mnemonic string) error {
	c, err := open(env, "unlock-recovery", needAgent)
	if err != nil {
		return err
	}
	defer c.close()
	return describe(vault.UnlockWithRecoveryPhrase(c.session, mnemonic), "unlock-recovery", "")
}
