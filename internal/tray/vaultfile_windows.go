package tray

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
	"github.com/ollykeran/sshush/internal/vaultops"
	"golang.org/x/sys/windows"
)

// The Vault page's file actions: copying the vault out, bringing one in, and
// switching the agent between a vault and key files.

// vaultActionsNow is vaultActions for the window's state, or none while an
// operation runs.
func (w *mainWindow) vaultActionsNow() VaultActions {
	if w.busy {
		return VaultActions{}
	}
	return vaultActions(w.state)
}

// backupVault asks where to, and copies the vault file there.
func (w *mainWindow) backupVault() {
	if !w.vaultActionsNow().Backup {
		return
	}
	src, hwnd := w.vault.Path, w.hwnd
	w.run("Backing up the vault…", func() opResult {
		dir := filepath.Dir(src)
		if home, err := os.UserHomeDir(); err == nil && isDir(filepath.Join(home, "Documents")) {
			dir = filepath.Join(home, "Documents")
		}
		dst, err := pickVaultBackup(hwnd, filepath.Join(dir, backupName(time.Now())))
		if errors.Is(err, errCancelled) {
			return opResult{}
		}
		if err != nil {
			return opResult{err: err}
		}
		sum, err := vaultops.Backup(src, dst)
		return done(fmt.Sprintf("Backed up the vault (%s) to %s.", sum, utils.DisplayPath(dst)), err)
	})
}

// importVault asks for a vault file and its passphrase, then makes it the
// vault: the agent is stopped, the file copied in with the old vault kept
// aside, config set to serve it, and the agent started and unlocked.
func (w *mainWindow) importVault() {
	if !w.vaultActionsNow().Import {
		return
	}
	hwnd, t := w.hwnd, w.t
	w.run("Importing a vault…", func() opResult {
		src, err := pickVaultFile(hwnd)
		if errors.Is(err, errCancelled) {
			return opResult{}
		}
		if err != nil {
			return opResult{err: err}
		}
		pass, sum, err := askVaultFilePassphrase(hwnd, src)
		if errors.Is(err, errCancelled) {
			return opResult{}
		}
		if err != nil {
			return opResult{err: err}
		}
		defer clear(pass)

		env := t.environment()
		target := env
		if target.VaultPath == "" {
			target.VaultPath = defaultVaultPath()
		}
		dst := vault.ResolveToFile(utils.ExpandHomeDirectory(target.VaultPath))
		_, statErr := os.Stat(dst)
		if messageBoxOwned(hwnd, importQuestion(src, sum, dst, statErr == nil), mbYesNo|windows.MB_ICONQUESTION) != idYes {
			return opResult{}
		}

		running := Probe(env.SocketPath).Err == nil
		if running {
			if err := StopAgent(); err != nil {
				return opResult{err: fmt.Errorf("could not stop the agent to import the vault: %w", err)}
			}
		}
		res, err := vaultops.Import(target, src, pass)
		if err != nil {
			if running {
				_ = StartAgent(env.SocketPath) // as it was
			}
			return opResult{err: err}
		}
		if env.AgentVaultPath == "" || env.VaultPath == "" {
			if err := config.WriteAgentTypeToPath(runtime.ConfigPathDefault(), config.AgentTypeVault, dst); err != nil {
				return opResult{err: fmt.Errorf("imported the vault, but could not set the agent to use it: %w", err)}
			}
			if err := t.reloadEnv(); err != nil {
				return opResult{err: fmt.Errorf("imported the vault, but could not read the config back: %w", err)}
			}
			env = t.environment()
		}
		if err := StartAgent(env.SocketPath); err != nil {
			return opResult{err: fmt.Errorf("imported the vault, but could not start the agent: %w", err)}
		}
		if err := vaultops.UnlockPassphrase(env, pass); err != nil && vaultops.CodeOf(err) != vaultops.CodeNotLocked {
			return opResult{err: fmt.Errorf("imported the vault, but could not unlock it: %w", err)}
		}
		return opResult{status: importedStatus(res)}
	})
}

// askVaultFilePassphrase asks for the passphrase of the vault file at path
// until one opens it or the user cancels, and returns it with what the file
// holds. Nothing is unlocked: the passphrase is only checked.
func askVaultFilePassphrase(owner uintptr, path string) ([]byte, vaultops.FileSummary, error) {
	spec := inputSpec{
		title:   "Import a vault",
		message: "Enter the passphrase of " + filepath.Base(path) + ". It is checked before anything changes.",
		okLabel: "Continue",
		masked:  true,
	}
	for {
		pass, button, err := promptInput(owner, spec)
		if err != nil {
			return nil, vaultops.FileSummary{}, err
		}
		if button != idOK {
			clear(pass)
			return nil, vaultops.FileSummary{}, errCancelled
		}
		sum, err := vaultops.CheckVaultFile(path, pass)
		if vaultops.CodeOf(err) == vaultops.CodeWrongPassphrase {
			clear(pass)
			spec.errText = "That passphrase does not open it. Try again."
			continue
		}
		if err != nil {
			clear(pass)
			return nil, vaultops.FileSummary{}, err
		}
		return pass, sum, nil
	}
}

// switchAgent switches the agent between serving a vault and serving key
// files, in config, and restarts it. A vault agent is unlocked with pass, or
// with none asks for the passphrase.
func switchAgent(t *tray, toVault bool, pass []byte) error {
	_, err := switchAgentStatus(t, toVault, pass)
	return err
}

// switchAgentStatus is switchAgent, with what it did for the status line.
func switchAgentStatus(t *tray, toVault bool, pass []byte) (string, error) {
	env := t.environment()
	kind := config.AgentTypeKeys
	if toVault {
		kind = config.AgentTypeVault
	}
	vaultFile := vault.ResolveToFile(utils.ExpandHomeDirectory(vaultEnv(env).VaultPath))
	_, statErr := os.Stat(vaultFile)
	exists := statErr == nil
	write := ""
	if toVault && env.VaultPath == "" {
		write = vaultFile
	}
	running := Probe(env.SocketPath).Err == nil
	if running {
		if err := StopAgent(); err != nil {
			return "", fmt.Errorf("could not stop the agent: %w", err)
		}
	}
	if err := config.WriteAgentTypeToPath(runtime.ConfigPathDefault(), kind, write); err != nil {
		if running {
			_ = StartAgent(env.SocketPath)
		}
		return "", fmt.Errorf("could not change the config: %w", err)
	}
	if err := t.reloadEnv(); err != nil {
		return "", fmt.Errorf("changed the config, but could not read it back: %w", err)
	}
	if toVault && !exists {
		// A vault agent needs its vault: it starts once one is created.
		return "The agent now uses a vault. Create it to add your keys.", nil
	}
	if err := StartAgent(env.SocketPath); err != nil {
		return "", fmt.Errorf("changed the config, but could not start the agent: %w", err)
	}
	if !toVault {
		return "The agent now serves your key files.", nil
	}
	if pass != nil {
		if err := vaultops.UnlockPassphrase(t.environment(), pass); err != nil {
			return "", err
		}
		return "The agent now serves your vault.", nil
	}
	if err := t.promptUnlock(); err != nil && !errors.Is(err, errCancelled) {
		return "", err
	}
	return "The agent now serves your vault.", nil
}
