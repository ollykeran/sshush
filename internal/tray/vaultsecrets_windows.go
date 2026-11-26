package tray

import (
	"errors"
	"strings"

	"github.com/ollykeran/sshush/internal/vault"
	"github.com/ollykeran/sshush/internal/vaultops"
)

// The Vault page's secret actions: a new passphrase, or a new recovery phrase.
// Each asks for the vault's passphrase, or its recovery phrase, to prove the
// change; being able to reach an unlocked agent is not enough.

// askCredential asks for the vault's passphrase under title, with a button to
// give the recovery phrase instead. errText, when set, says why it is asking
// again.
func askCredential(owner uintptr, title, errText string) (vault.Credential, error) {
	pass, button, err := promptInput(owner, inputSpec{
		title:      title,
		message:    "Enter the vault's current passphrase.",
		errText:    errText,
		okLabel:    "Next",
		extraLabel: "Use &recovery phrase…",
		masked:     true,
	})
	if err != nil {
		return vault.Credential{}, err
	}
	switch button {
	case idOK:
		return vault.Credential{Passphrase: pass}, nil
	case idExtra:
		clear(pass)
	default:
		clear(pass)
		return vault.Credential{}, errCancelled
	}
	b, button, err := promptInput(owner, inputSpec{
		title:   title,
		message: "Enter the vault's 24-word recovery phrase, separated by spaces.",
		okLabel: "Next",
	})
	if err != nil {
		return vault.Credential{}, err
	}
	phrase := strings.Join(strings.Fields(strings.ToLower(string(b))), " ")
	clear(b)
	if button != idOK {
		return vault.Credential{}, errCancelled
	}
	return vault.Credential{Mnemonic: phrase}, nil
}

// withCredential asks for a credential and runs change with it, asking again
// while it is the wrong one.
func withCredential(owner uintptr, title string, change func(vault.Credential) error) error {
	errText := ""
	for {
		cred, err := askCredential(owner, title, errText)
		if err != nil {
			return err
		}
		err = change(cred)
		clear(cred.Passphrase)
		switch vaultops.CodeOf(err) {
		case vaultops.CodeWrongPassphrase:
			errText = "That does not unlock this vault. Try again."
			continue
		case vaultops.CodeNoRecovery:
			errText = "This vault has no recovery phrase. Enter its passphrase."
			continue
		}
		return err
	}
}

// changePassphrase asks for the current secret and a new passphrase, changes
// it, and shows the vault's new recovery phrase once.
func (w *mainWindow) changePassphrase() {
	if !w.vaultActionsNow().ChangePassphrase {
		return
	}
	hwnd, t := w.hwnd, w.t
	const title = "Change passphrase"
	w.run("Changing the passphrase…", func() opResult {
		var res vaultops.SecretResult
		var newPass []byte
		defer func() { clear(newPass) }()
		err := withCredential(hwnd, title, func(cred vault.Credential) error {
			if newPass == nil {
				p, err := askNewPassphraseFor(hwnd, title,
					"Choose the new passphrase. Every key in the vault is encrypted again under it, and the vault gets a new recovery phrase.",
					"Change")
				if err != nil {
					return err
				}
				newPass = p
			}
			var err error
			res, err = vaultops.ChangePassphrase(t.environment(), cred, newPass, true)
			return err
		})
		if errors.Is(err, errCancelled) {
			return opResult{}
		}
		if err != nil {
			return opResult{err: err}
		}
		showRecoveryPhrase(hwnd, replacedRecoveryMessage(res.RecoveryFile), res.Mnemonic)
		return opResult{status: "Changed the vault's passphrase. Use the new one from now on."}
	})
}

// newRecoveryPhrase asks for the current secret, replaces the recovery
// phrase, and shows the new one once.
func (w *mainWindow) newRecoveryPhrase() {
	if !w.vaultActionsNow().NewRecovery {
		return
	}
	hwnd, t := w.hwnd, w.t
	w.run("Making a new recovery phrase…", func() opResult {
		var res vaultops.SecretResult
		err := withCredential(hwnd, "New recovery phrase", func(cred vault.Credential) error {
			var err error
			res, err = vaultops.RegenerateRecovery(t.environment(), cred)
			return err
		})
		if errors.Is(err, errCancelled) {
			return opResult{}
		}
		if err != nil {
			return opResult{err: err}
		}
		showRecoveryPhrase(hwnd, replacedRecoveryMessage(res.RecoveryFile), res.Mnemonic)
		return opResult{status: "Made a new recovery phrase. The old one no longer works."}
	})
}
