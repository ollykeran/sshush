package tray

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"unsafe"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/platform"
	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
	"golang.org/x/sys/windows"
)

// The Settings page's form: what config.toml says, what the page says now,
// and what is wrong with it. Edits stay on the page until Save, which writes
// only the lines that changed, and only a file that loads.

const (
	lbnSelChange   = 1 // LBN_SELCHANGE
	lbAddString    = 0x0180
	lbDeleteString = 0x0182
	lbSetCurSel    = 0x0186
	lbGetCurSel    = 0x0188
	lbResetContent = 0x0184
	cbResetContent = 0x014B
	mbYesNoCancel  = 0x3
	idNo           = 7
)

// settingsForm is the Settings page's state.
type settingsForm struct {
	loaded      bool
	path        string
	saved, edit config.CoreSettings
	problems    []FieldProblem
	// filling is set while the page's controls are being filled, so their
	// change notifications are not taken for the user's edits.
	filling bool
}

// dirty reports whether the page says something the file does not.
func (f *settingsForm) dirty() bool { return f.loaded && !f.edit.Equal(f.saved) }

// loadSettings reads config.toml into the page, creating it with the
// defaults when there is none yet.
func (w *mainWindow) loadSettings() {
	f := &w.settings
	f.path = runtime.ConfigPathDefault()
	if _, err := os.Stat(f.path); os.IsNotExist(err) {
		if err := config.WriteDefaultConfigFile(f.path, false); err != nil {
			f.loaded = false
			w.setStatus("Could not create a config file: "+err.Error(), true)
			return
		}
	}
	s, err := config.ReadCoreSettings(f.path)
	if err != nil {
		f.loaded = false
		w.setStatus(oneLine(err.Error())+". Fix the file, then open the Settings tab again.", true)
		return
	}
	f.loaded, f.saved, f.edit = true, s, cloneSettings(s)
	w.fillSettings()
}

func cloneSettings(s config.CoreSettings) config.CoreSettings {
	s.KeyPaths, s.SearchDirs = slices.Clone(s.KeyPaths), slices.Clone(s.SearchDirs)
	return s
}

// fillSettings puts the form into the page's controls.
func (w *mainWindow) fillSettings() {
	f := &w.settings
	f.filling = true
	defer func() { f.filling = false }()

	combo := control(w.hwnd, idSetType)
	procSendMessageW.Call(combo, cbResetContent, 0, 0)
	for i, c := range agentTypeChoices(f.saved.AgentType) {
		p, _ := windows.UTF16PtrFromString(c.label)
		procSendMessageW.Call(combo, cbAddString, 0, uintptr(unsafe.Pointer(p)))
		if c.value == f.edit.AgentType {
			procSendMessageW.Call(combo, cbSetCurSel, uintptr(i), 0)
		}
	}
	setText(control(w.hwnd, idSetSocket), f.edit.SocketPath)
	procSendMessageW.Call(control(w.hwnd, idSetPageant), bmSetCheck, boolArg(f.edit.PageantEmulation), 0)
	setText(control(w.hwnd, idSetVaultPath), f.edit.VaultPath)
	fillList(control(w.hwnd, idSetKeysList), f.edit.KeyPaths)
	fillList(control(w.hwnd, idSetDirsList), f.edit.SearchDirs)
	w.updateSettings()
}

func fillList(list uintptr, items []string) {
	procSendMessageW.Call(list, lbResetContent, 0, 0)
	for _, it := range items {
		p, _ := windows.UTF16PtrFromString(it)
		procSendMessageW.Call(list, lbAddString, 0, uintptr(unsafe.Pointer(p)))
	}
}

// updateSettings checks the form and shows what it found: each caption, or
// the problem in its place, and Save only when there is something to save.
func (w *mainWindow) updateSettings() {
	f := &w.settings
	f.problems = settingsProblems(f.edit)
	for _, id := range []uint16{idSetTypeCaption, idSetSocketCaption, idSetPageantCaption, idSetVaultCaption, idSetKeysCaption, idSetDirsCaption} {
		text, _ := settingsCaption(id, f.problems)
		h := control(w.hwnd, int(id))
		setText(h, text)
		procRedrawWindow.Call(h, 0, 0, rdwInvalidate|rdwErase) // its colour may change
	}
	on := f.loaded && !w.busy
	for _, id := range []uint16{idSetType, idSetSocket, idSetSocketDefault, idSetPageant, idSetVaultPath, idSetVaultBrowse, idSetKeysList, idSetKeysAdd, idSetDirsList, idSetDirsAdd} {
		procEnableWindow.Call(control(w.hwnd, int(id)), boolArg(on))
	}
	procEnableWindow.Call(control(w.hwnd, idSetSocketDefault), boolArg(on && f.edit.SocketPath != platform.DefaultSocketPath()))
	procEnableWindow.Call(control(w.hwnd, idSetKeysRemove), boolArg(on && listSelection(control(w.hwnd, idSetKeysList)) >= 0))
	procEnableWindow.Call(control(w.hwnd, idSetDirsRemove), boolArg(on && listSelection(control(w.hwnd, idSetDirsList)) >= 0))
	procEnableWindow.Call(control(w.hwnd, idSetDiscard), boolArg(on && f.dirty()))
	procEnableWindow.Call(control(w.hwnd, idSetSave), boolArg(on && f.dirty() && !blocked(f.problems)))
}

// settingsProblemAt is the problem shown in the caption control h, if any.
func (w *mainWindow) settingsProblemAt(h uintptr) *FieldProblem {
	id, _, _ := procGetDlgCtrlID.Call(h)
	_, p := settingsCaption(uint16(id), w.settings.problems)
	return p
}

func listSelection(list uintptr) int {
	r, _, _ := procSendMessageW.Call(list, lbGetCurSel, 0, 0)
	return int(int32(r))
}

// settingsCommand handles a notification from one of the Settings page's
// controls, reporting whether it was one.
func (w *mainWindow) settingsCommand(id, code uint16) bool {
	f := &w.settings
	if id < idSetAgentHeader || id > idSetDiscard {
		return false
	}
	if f.filling || !f.loaded {
		return true
	}
	switch {
	case id == idSetType && code == cbnSelChange:
		r, _, _ := procSendMessageW.Call(control(w.hwnd, idSetType), cbGetCurSel, 0, 0)
		if c := agentTypeChoices(f.saved.AgentType); int(int32(r)) >= 0 && int(int32(r)) < len(c) {
			f.edit.AgentType = c[int32(r)].value
		}
		if f.edit.AgentType == config.AgentTypeVault && f.edit.VaultPath == "" {
			// Where the Vault page creates one, when config names none.
			f.edit.VaultPath = homePath(defaultVaultPath())
			w.fillSettings()
		}
	case id == idSetSocket && code == enChange:
		f.edit.SocketPath = windowText(control(w.hwnd, idSetSocket))
	case id == idSetVaultPath && code == enChange:
		f.edit.VaultPath = windowText(control(w.hwnd, idSetVaultPath))
	case (id == idSetKeysList || id == idSetDirsList) && code == lbnSelChange:
	case code != bnClicked:
		return true
	case id == idSetSocketDefault:
		f.edit.SocketPath = platform.DefaultSocketPath()
		w.fillSettings()
	case id == idSetPageant:
		r, _, _ := procSendMessageW.Call(control(w.hwnd, idSetPageant), bmGetCheck, 0, 0)
		f.edit.PageantEmulation = r == 1
	case id == idSetVaultBrowse:
		w.browseVaultPath()
	case id == idSetKeysAdd:
		w.addSettingsKeys()
	case id == idSetKeysRemove:
		f.edit.KeyPaths = removeAt(f.edit.KeyPaths, listSelection(control(w.hwnd, idSetKeysList)))
		w.fillSettings()
	case id == idSetDirsAdd:
		w.addSettingsFolder()
	case id == idSetDirsRemove:
		f.edit.SearchDirs = removeAt(f.edit.SearchDirs, listSelection(control(w.hwnd, idSetDirsList)))
		w.fillSettings()
	case id == idSetDiscard:
		f.edit = cloneSettings(f.saved)
		w.fillSettings()
		w.setStatus("Discarded the changes to the settings.", false)
	case id == idSetSave:
		w.saveSettings()
	}
	w.updateSettings()
	return true
}

func removeAt(list []string, i int) []string {
	if i < 0 || i >= len(list) {
		return list
	}
	return slices.Delete(slices.Clone(list), i, i+1)
}

func (w *mainWindow) browseVaultPath() {
	f := &w.settings
	suggested := f.edit.VaultPath
	if suggested == "" {
		suggested = defaultVaultPath()
	}
	path, err := pickVaultLocation(w.hwnd, vault.ResolveToFile(utils.ExpandHomeDirectory(suggested)))
	if errors.Is(err, errCancelled) {
		return
	}
	if err != nil {
		w.setStatus(err.Error(), true)
		return
	}
	f.edit.VaultPath = homePath(path)
	w.fillSettings()
}

func (w *mainWindow) addSettingsKeys() {
	paths, err := pickKeyFiles()
	if errors.Is(err, errCancelled) {
		return
	}
	if err != nil {
		w.setStatus(err.Error(), true)
		return
	}
	for i := range paths {
		paths[i] = homePath(paths[i])
	}
	var n int
	w.settings.edit.KeyPaths, n = addPaths(w.settings.edit.KeyPaths, paths...)
	w.fillSettings()
	if n < len(paths) {
		w.setStatus("Some of those files are listed already.", false)
	}
}

func (w *mainWindow) addSettingsFolder() {
	dir, err := pickFolder(w.hwnd, "Choose a folder to search for keys")
	if errors.Is(err, errCancelled) {
		return
	}
	if err != nil {
		w.setStatus(err.Error(), true)
		return
	}
	var n int
	w.settings.edit.SearchDirs, n = addPaths(w.settings.edit.SearchDirs, homePath(dir))
	w.fillSettings()
	if n == 0 {
		w.setStatus("That folder is listed already.", false)
	}
}

// saveSettings writes the form to config.toml, and offers to restart the
// agent when that is what applies the change. It reports whether it saved.
func (w *mainWindow) saveSettings() bool {
	f := &w.settings
	if !f.dirty() {
		return true
	}
	if problems := settingsProblems(f.edit); blocked(problems) {
		for _, p := range problems {
			if p.Blocker {
				w.setStatus("Not saved: "+p.Text, true)
				break
			}
		}
		return false
	}
	if err := config.WriteCoreSettingsToPath(f.path, f.edit); err != nil {
		w.setStatus("Not saved: "+oneLine(err.Error()), true)
		return false
	}
	old := f.saved
	f.saved = cloneSettings(f.edit)
	w.updateSettings()
	t := w.t
	if err := t.reloadEnv(); err != nil {
		w.setStatus("Saved, but could not read the config back: "+oneLine(err.Error()), true)
		return true
	}
	note := ""
	if !strings.EqualFold(old.SocketPath, f.edit.SocketPath) {
		if canRestart(w.state) {
			// The agent serves the old pipe until Restart agent moves it.
			w.restartPending = true
			note = " Restart the agent to move it to the new pipe."
		} else if aligned, err := t.adoptConfigSocket(); err != nil {
			w.setStatus("Saved, but "+oneLine(err.Error()), true)
			w.run("", nil)
			return true
		} else {
			note = " The agent will use the new pipe when it starts." + alignedNote(aligned)
		}
	}
	if note == "" && needsRestart(old, f.edit) && canRestart(w.state) {
		// The Restart agent button, beside Stop, applies them.
		w.restartPending = true
		note = " Restart the agent to apply them."
	}
	if note == "" && needsRestart(old, f.edit) && !canRestart(w.state) {
		note = " They apply when the agent next starts."
	}
	w.setStatus(settingsSavedStatus(false)+note, false)
	w.run("", nil)
	return true
}

// leaveSettings asks what to do with unsaved settings before the page is
// left, and reports whether it may be.
func (w *mainWindow) leaveSettings() bool {
	if !w.settings.dirty() {
		return true
	}
	switch messageBoxOwned(w.hwnd, "Save the changes to the settings?", mbYesNoCancel|windows.MB_ICONQUESTION) {
	case idYes:
		return w.saveSettings()
	case idNo:
		w.settings.edit = cloneSettings(w.settings.saved)
		w.fillSettings()
		return true
	}
	return false
}

// restartAgent stops the agent if it runs and starts it again from config,
// on the pipe config names now, unlocking a vault it serves. A vault agent
// whose vault does not exist yet is left stopped: it starts once the vault is
// created. aligned is what adoptConfigSocket changed to follow a new pipe.
func restartAgent(t *tray) (aligned []string, err error) {
	if Probe(t.environment().SocketPath).Err == nil {
		if err := StopAgent(); err != nil {
			return nil, fmt.Errorf("could not stop the agent: %w", err)
		}
	}
	if aligned, err = t.adoptConfigSocket(); err != nil {
		return aligned, err
	}
	env := t.environment()
	if env.AgentVaultPath != "" {
		if _, err := os.Stat(vault.ResolveToFile(utils.ExpandHomeDirectory(env.AgentVaultPath))); err != nil {
			return aligned, nil
		}
	}
	if err := StartAgent(env.SocketPath); err != nil {
		return aligned, fmt.Errorf("could not start the agent: %w", err)
	}
	if env.AgentVaultPath != "" {
		if err := t.promptUnlock(); err != nil && !errors.Is(err, errCancelled) {
			return aligned, err
		}
	}
	return aligned, nil
}
