package server

import (
	"os"

	"github.com/ollykeran/sshush/internal/vault"
)

// PasswordSource checks a password presented by an SSH client. How long a check
// takes must not depend on how close the password was.
type PasswordSource interface {
	Verify(password []byte) bool
}

// VaultPassphraseAuth implements PasswordSource by checking the password against
// the passphrase of the vault at VaultPath — the secret that already opens the vault
// everywhere else — without unlocking anything. The agent is neither asked nor
// changed: a locked agent stays locked, and signing in over SSH is not a way to
// unlock it.
//
// The vault file is read for each check, as SocketAuth dials for each check, so a
// passphrase changed while the server runs is the one accepted from then on.
type VaultPassphraseAuth struct {
	VaultPath string
}

// Verify reports whether password is the vault's passphrase. A vault that is
// missing, unreadable or not initialized accepts nothing.
func (v *VaultPassphraseAuth) Verify(password []byte) bool {
	if v.VaultPath == "" {
		return false
	}
	// vault.Open creates a missing vault's directory, which checking a password a
	// stranger sent should never do.
	if _, err := os.Stat(v.VaultPath); err != nil {
		return false
	}
	store, err := vault.Open(v.VaultPath)
	if err != nil {
		return false
	}
	return vault.VerifyPassphrase(store, password)
}
