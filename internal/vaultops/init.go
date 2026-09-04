package vaultops

import (
	"os"
	"path/filepath"

	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
)

// InitOptions controls what Init does beyond creating the vault.
type InitOptions struct {
	// Recovery generates a 24-word BIP-39 recovery phrase, enables recovery on
	// the vault, and writes recovery.txt (mode 0600) beside the vault file.
	Recovery bool
	// RecoveryFile, when set, receives a second copy of the phrase (mode 0600).
	// It is ignored when Recovery is false.
	RecoveryFile string
}

// InitResult reports where the vault was created and, when recovery was
// enabled, the phrase and the files holding it. Mnemonic is the only copy in
// memory: show it once, and neither log nor store it.
type InitResult struct {
	VaultPath    string
	Mnemonic     string // empty when InitOptions.Recovery was false
	RecoveryFile string // recovery.txt beside the vault
	ExtraFile    string // the copy at InitOptions.RecoveryFile, if any
}

// InitTarget resolves the vault file Init would create and reports whether it
// can be created, so a front end can fail before prompting for a passphrase.
// The resolved path is returned even when creation is impossible, so the
// failure can name it.
func InitTarget(env Env) (string, error) {
	path := env.vaultFile()
	if path == "" {
		return "", &OpError{
			Code: CodeNoVaultPath,
			Msg:  "vault path required",
			Hint: "Set [vault].vault_path in config, or pass --vault-path.",
		}
	}
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		return path, &OpError{
			Code: CodeVaultExists,
			Msg:  "vault already exists at " + utils.DisplayPath(path),
		}
	}
	return path, nil
}

// Init creates a vault at env's vault path, protected by passphrase, and
// optionally enables recovery. The caller owns passphrase and should zero it;
// Init does not.
func Init(env Env, passphrase []byte, opts InitOptions) (InitResult, error) {
	path, err := InitTarget(env)
	if err != nil {
		return InitResult{}, err
	}
	store, err := vault.Open(path)
	if err != nil {
		return InitResult{}, &OpError{Code: CodeVaultUnreadable, Msg: "create vault: " + err.Error(), Err: err}
	}
	if err := vault.Init(store, passphrase); err != nil {
		return InitResult{}, &OpError{Code: CodeLocalIO, Msg: "init vault: " + err.Error(), Err: err}
	}

	res := InitResult{VaultPath: path}
	if !opts.Recovery {
		return res, nil
	}
	mnemonic, err := vault.GenerateRecoveryMnemonic()
	if err != nil {
		return res, &OpError{Code: CodeLocalIO, Msg: "generate recovery phrase: " + err.Error(), Err: err}
	}
	if err := vault.EnableRecoveryWithPassphrase(store, passphrase, mnemonic); err != nil {
		return res, &OpError{Code: CodeLocalIO, Msg: "enable recovery: " + err.Error(), Err: err}
	}
	recoveryTxt := filepath.Join(filepath.Dir(path), "recovery.txt")
	if err := os.WriteFile(recoveryTxt, []byte(mnemonic+"\n"), 0600); err != nil {
		return res, &OpError{Code: CodeLocalIO, Msg: "write recovery.txt: " + err.Error(), Err: err}
	}
	res.Mnemonic = mnemonic
	res.RecoveryFile = recoveryTxt

	if opts.RecoveryFile != "" {
		extra := utils.ExpandHomeDirectory(opts.RecoveryFile)
		if err := os.WriteFile(extra, []byte(mnemonic+"\n"), 0600); err != nil {
			return res, &OpError{Code: CodeLocalIO, Msg: "write recovery file: " + err.Error(), Err: err}
		}
		res.ExtraFile = extra
	}
	return res, nil
}
