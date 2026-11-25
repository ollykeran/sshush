package tray

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vaultops"
)

// The Vault tab is about the vault itself rather than the keys in it: where
// it is, what state it is in, and what can be done to it as a whole. What it
// shows is worked out here from a MainState, so it is tested on every OS;
// manage_windows.go only draws it.

// VaultRow is one labelled fact on the Vault tab.
type VaultRow struct{ Label, Value string }

// VaultView is what the Vault tab shows.
type VaultView struct {
	// Title names the tab's state in a word or two; Sub says what it means.
	Title, Sub string
	Rows       []VaultRow
	// Path is the vault file, or where one would be created; "" when there is
	// none to point at.
	Path string
}

// VaultActions says which of the Vault tab's buttons do anything.
type VaultActions struct {
	CreateVault, Lock, Unlock bool
	OpenFolder, CopyPath      bool
	// Backup copies the vault file elsewhere; Import brings one in from
	// elsewhere, in place of the vault there is now, if any.
	Backup, Import bool
	// ChangePassphrase and NewRecovery change the vault's secrets.
	ChangePassphrase, NewRecovery bool
	// UseVault and UseKeys switch what the agent serves, in config, and
	// restart it. At most one is set.
	UseVault, UseKeys bool
}

// vaultPath is the vault file the window knows of: the one listed, else the
// configured one.
func (s MainState) vaultPath() string {
	if s.Listing && s.ListErr == nil && s.Listed.VaultPath != "" {
		return s.Listed.VaultPath
	}
	return s.ConfiguredVault
}

// vaultOf describes the vault for the Vault tab.
func vaultOf(s MainState) VaultView {
	v := VaultView{Title: "Vault", Path: s.vaultPath()}
	locked := s.isVault() && s.Backend.VaultLocked
	switch {
	case s.Reachable && !s.isVault():
		v.Title = "Not using a vault"
		v.Sub = "The agent serves key files. A vault keeps your keys encrypted in one file you can take to another PC: use one, or import one you have."
	case s.needsVault():
		v.Title = "No vault yet"
		v.Sub = "Create one to keep your keys encrypted in a single file you can take to another PC."
	case s.ListErr != nil && !(locked && vaultops.CodeOf(s.ListErr) == vaultops.CodeVaultLocked):
		v.Sub = describeLine(s.ListErr)
	case !s.Reachable:
		v.Title = "Vault · agent stopped"
		v.Sub = "Start the agent to unlock the vault and use its keys."
	case locked:
		v.Title = "Vault · locked"
		v.Sub = "Its keys are encrypted and out of the agent. Unlock it to use them."
	default:
		v.Title = "Vault · unlocked"
		v.Sub = "Its keys are available to ssh. Lock it to take them out of memory."
	}
	if v.Path == "" {
		return v
	}
	v.Rows = append(v.Rows, VaultRow{"Location", utils.DisplayPath(v.Path)})
	if !s.showsVault() {
		return v
	}
	if s.FileFound {
		v.Rows = append(v.Rows,
			VaultRow{"Size", byteSize(s.FileSize)},
			VaultRow{"Last changed", whenLabel(s.FileModTime)})
	}
	if !s.Listed.Created.IsZero() {
		v.Rows = append(v.Rows, VaultRow{"Created", whenLabel(s.Listed.Created)})
	}
	v.Rows = append(v.Rows, VaultRow{"Keys", keysLabel(s.Listed)}, VaultRow{"Recovery phrase", recoveryLabel(s.Listed.Recovery)})
	return v
}

// vaultActions works out VaultActions for s.
func vaultActions(s MainState) VaultActions {
	a := mainActions(s, nil)
	path := s.vaultPath() != ""
	return VaultActions{
		CreateVault: a.CreateVault,
		Lock:        a.Lock,
		Unlock:      a.Unlock,
		OpenFolder:  path,
		CopyPath:    path,
		Backup:      s.showsVault() && s.FileFound,
		// Either goes through the agent serving the vault, or with none,
		// to its file.
		ChangePassphrase: s.showsVault(),
		NewRecovery:      s.showsVault(),
		// Importing restarts the agent, as switching does, so both need an
		// agent this sshush runs.
		Import:   s.Managed,
		UseVault: s.Managed && !s.ConfigIsVault,
		UseKeys:  s.Managed && s.ConfigIsVault,
	}
}

// modeLabel names the mode button by what pressing it does.
func modeLabel(useKeys bool) string {
	if useKeys {
		return "Use key files instead"
	}
	return "Use a vault"
}

// keysLabel counts a vault's keys and, when the agent says, how many it is
// serving.
func keysLabel(l vaultops.ListResult) string {
	text := plural(len(l.Identities), "key")
	if !l.AgentReachable || len(l.Identities) == 0 {
		return text
	}
	loaded := 0
	for _, id := range l.Identities {
		if id.Loaded == vaultops.LoadYes {
			loaded++
		}
	}
	return fmt.Sprintf("%s, %d loaded", text, loaded)
}

func recoveryLabel(r vaultops.RecoveryState) string {
	switch r {
	case vaultops.RecoveryOn:
		return "Set up: the phrase can unlock the vault"
	case vaultops.RecoveryOff:
		return "Not set up: only the passphrase unlocks the vault"
	}
	return "Unknown: the agent does not say"
}

// byteSize is n bytes as Explorer would put it.
func byteSize(n int64) string {
	const k = 1024
	switch {
	case n < k:
		return plural(int(n), "byte")
	case n < k*k:
		return fmt.Sprintf("%.1f KB", float64(n)/k)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(k*k))
	}
}

// whenLabel is t in local time, to the minute.
func whenLabel(t time.Time) string {
	return t.Local().Format("2 Jan 2006, 15:04")
}

// backupName is the file name a backup made at t is offered under.
func backupName(t time.Time) string {
	return "vault-" + t.Format("20060102") + ".json"
}

// importQuestion asks whether to import the vault at src, which sum
// describes, as the vault at dst. replacing says a vault is there already.
func importQuestion(src string, sum vaultops.FileSummary, dst string, replacing bool) string {
	text := fmt.Sprintf("Use %s (%s) as your vault?\n\nThe agent restarts with it, unlocked.", utils.DisplayPath(src), sum)
	if replacing {
		text += fmt.Sprintf(" The vault you have now is kept beside it, renamed from %s to %s.bak-<date>, and its recovery phrase no longer applies.",
			filepath.Base(dst), filepath.Base(dst))
	}
	return text
}

// importedStatus is the status line after an import.
func importedStatus(res vaultops.ImportResult) string {
	text := fmt.Sprintf("Imported the vault (%s) and unlocked it.", res.FileSummary)
	if res.Previous != "" {
		text += " The old one is " + filepath.Base(res.Previous) + "."
	}
	return text
}

// switchQuestion asks whether to switch the agent to a vault, or to key files.
func switchQuestion(toVault, vaultExists bool) string {
	if !toVault {
		return "Serve your key files instead of the vault?\n\nThe agent restarts, and the vault's keys leave memory. The vault itself is kept, and you can switch back."
	}
	if !vaultExists {
		return "Keep your keys in a vault?\n\nThe agent stops serving your key files. Create the vault next, then add your keys to it."
	}
	return "Serve keys from your vault instead of key files?\n\nThe agent restarts, and asks for the vault's passphrase."
}
