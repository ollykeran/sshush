package tray

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vaultops"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// The main window is where everything the tray does lives: the agent's
// state, starting and stopping it, locking and unlocking a vault, and every
// key, with actions on the one selected, as the TUI's Agent and Vault tabs
// do. The tray's menu only shows the state and opens this. What the window
// shows is worked out here from a MainState, so it is tested on every OS;
// manage_windows.go only draws it.

// Main window item IDs. idCancel closes it, as Esc and the close button do.
const (
	idMainAgent = 200 + iota
	idMainAgentToggle
	idMainLock
	idMainSource
	idMainList
	idMainStatus
	idMainAdd
	idMainRemove
	idMainLoad
	idMainUnload
	idMainAutoload
	idMainSelftest
	idMainThemeLabel
	idMainTheme
	idMainRefresh
	idMainStateIcon
)

// Styles the main window adds to those in dialog_template.go.
const (
	wsMinimizeBox   = 0x00020000
	wsBorder        = 0x00800000
	wsVScroll       = 0x00200000
	lvsReport       = 0x0001
	lvsSingleSel    = 0x0004
	lvsShowSelAlway = 0x0008
	lvsNoSortHeader = 0x8000
	ssIcon          = 0x0003
	ssCenterImage   = 0x0200
	ssEndEllipsis   = 0x4000
	ssPathEllipsis  = 0x8000
	cbsDropDownList = 0x0003
	classListView   = "SysListView32"
	classComboBox   = 0x85
)

// mainColumns heads the key list; keyRow fills it in this order.
var mainColumns = []string{"Comment", "Type", "Fingerprint", "Loaded", "Autoload", "Added from"}

// buttonGlyphs gives each button its icon: a code point in Windows' icon font,
// Segoe Fluent Icons or, before Windows 11, Segoe MDL2 Assets, which share
// these. The lock and agent buttons change icon with what they do; see
// lockGlyph and agentGlyph.
var buttonGlyphs = map[uint16]rune{
	idMainAdd:      0xE710, // Add
	idMainRemove:   0xE738, // Remove
	idMainLoad:     0xE896, // Download: into the agent
	idMainUnload:   0xE898, // Upload: out of it
	idMainAutoload: 0xE945, // LightningBolt
	idMainSelftest: 0xE9D9, // Diagnostic
	idMainRefresh:  0xE72C, // Refresh
}

// buttonRows are the window's rows of controls that fit their labels: each
// button is as wide as its icon and label need, and a row keeps its first
// control where the template put it, or with right set its last, so it grows
// away from the window's edge.
var buttonRows = []struct {
	ids   []uint16
	right bool
}{
	{[]uint16{idMainLock, idMainAgentToggle}, true},
	{[]uint16{idMainAdd, idMainRemove, idMainLoad, idMainUnload, idMainAutoload}, false},
	{[]uint16{idMainRefresh}, true},
	{[]uint16{idMainSelftest, idMainThemeLabel, idMainTheme}, false},
	{[]uint16{idCancel}, true},
}

// lockGlyph is the lock button's icon: what pressing it does.
func lockGlyph(a MainActions) rune {
	if a.Unlock {
		return 0xE785 // Unlock
	}
	return 0xE72E // Lock
}

// agentGlyph is the start/stop button's icon.
func agentGlyph(a MainActions) rune {
	if a.Start {
		return 0xE768 // Play
	}
	return 0xE71A // Stop
}

// mainTemplate lays out the main window as three bands: a header naming the
// agent, with its state icon and the buttons that change that state; the
// keys, a toolbar of what can be done to the one selected over the list and
// a status line under it; and a footer with the selftest, the theme and
// Close. Tab order follows that reading order.
func mainTemplate() []uint16 {
	const (
		w       = 480
		h       = 248
		margin  = 8
		btnH    = 16
		gap     = 4
		btnW    = 58
		wideW   = 80
		headerH = 30
		toolY   = margin + headerH + 6
		listY   = toolY + btnH + 4
		statusY = h - margin - btnH - 16
		footY   = h - margin - btnH
		textX   = margin + 26 // past the state icon
	)
	btn := func(id uint16, x, y, cx int16, text string) dlgItem {
		return dlgItem{style: wsChild | wsVisible | wsTabStop, x: x, y: y, cx: cx, cy: btnH, id: id, class: classButton, text: text}
	}
	label := func(id uint16, x, y, cx, cy int16, style uint32, text string) dlgItem {
		return dlgItem{style: wsChild | wsVisible | style, x: x, y: y, cx: cx, cy: cy, id: id, class: classStatic, text: text}
	}
	toggleX := int16(w - margin - wideW)
	lockX := toggleX - gap - btnW
	x := int16(margin)
	tool := func(id uint16, cx int16, text string) dlgItem {
		item := btn(id, x, toolY, cx, text)
		x += cx + gap
		return item
	}
	items := []dlgItem{
		// Header.
		label(idMainStateIcon, margin, margin+3, 20, 20, ssIcon|ssCenterImage, ""),
		label(idMainAgent, textX, margin, lockX-gap-textX, 16, ssNoPrefix|ssEndEllipsis, ""),
		label(idMainSource, textX, margin+18, lockX-gap-textX, 10, ssNoPrefix|ssPathEllipsis, ""),
		btn(idMainLock, lockX, margin+5, btnW, "&Lock"),
		btn(idMainAgentToggle, toggleX, margin+5, wideW, "S&top agent"),
		// Keys.
		tool(idMainAdd, btnW, "&Add…"),
		tool(idMainRemove, btnW+8, "&Remove…"),
		tool(idMainLoad, btnW, "L&oad"),
		tool(idMainUnload, btnW, "&Unload"),
		tool(idMainAutoload, 90, autoloadLabel(false)),
		btn(idMainRefresh, w-margin-btnW, toolY, btnW, "Refresh"),
		{style: wsChild | wsVisible | wsTabStop | wsBorder | lvsReport | lvsSingleSel | lvsShowSelAlway | lvsNoSortHeader,
			x: margin, y: listY, cx: w - 2*margin, cy: statusY - 4 - listY, id: idMainList, className: classListView},
		label(idMainStatus, margin, statusY, w-2*margin, 10, ssNoPrefix|ssEndEllipsis, ""),
		// Footer.
		btn(idMainSelftest, margin, footY, wideW, "Run s&elftest"),
		// The label's mnemonic moves the focus to the drop-down after it.
		label(idMainThemeLabel, margin+wideW+3*gap, footY+4, 26, 10, 0, "T&heme:"),
		// A drop-down list's height includes its open list.
		{style: wsChild | wsVisible | wsTabStop | wsVScroll | cbsDropDownList, x: margin + wideW + 3*gap + 26, y: footY + 1, cx: 80, cy: 60, id: idMainTheme, class: classComboBox},
		btn(idCancel, w-margin-btnW, footY, btnW, "Close"),
	}
	style := uint32(wsPopup | wsCaption | wsSysMenu | wsMinimizeBox | dsSetFont | dsSetForeground | dsCenter)
	return buildTemplate(style, w, h, "sshush", "Segoe UI", 9, items)
}

// Tone colours the agent's state in the header.
type Tone int

// The header's tones, after Windows' own status colours.
const (
	ToneNeutral Tone = iota // an agent sshush does not own: nothing to judge
	ToneGood                // serving keys
	ToneWarn                // a locked vault: running, but serving nothing
	ToneBad                 // nothing answers
)

// headerOf is the header's tone and state icon for s. The icon is the tray's
// own, so the window and the notification area agree.
func headerOf(s MainState) (Tone, Icon) {
	switch {
	case !s.Reachable:
		return ToneBad, IconUnreachable
	case s.isVault() && s.Backend.VaultLocked:
		return ToneWarn, IconLocked
	case s.isVault():
		return ToneGood, IconUnlocked
	case s.Backend.SpeaksOps:
		return ToneGood, IconKeys
	}
	return ToneNeutral, IconKeys
}

// MainState is what the main window last learned about the agent and the
// keys it can show.
type MainState struct {
	// Managed reports that sshush starts and stops this agent.
	Managed bool
	// Reachable reports that an agent answered; Backend is what it said.
	Reachable bool
	Backend   agent.Backend
	// Keys is what the agent serves, for an agent that is not a vault.
	Keys []*sshagent.Key
	// Listed is the vault's contents, when there is a vault to list: the
	// agent's, or with no agent the configured file. Valid when ListErr is
	// nil, and only when Listing is set.
	Listing bool
	Listed  vaultops.ListResult
	ListErr error
}

// ProbeMain asks the agent what it is and what it holds, and lists the vault
// when there is one: the agent's own, or the configured file when no agent
// answers.
func ProbeMain(env vaultops.Env) MainState {
	s := MainState{Managed: !env.AgentUnmanaged}
	if snap := Probe(env.SocketPath); snap.Err == nil {
		s.Reachable, s.Backend, s.Keys = true, snap.Backend, snap.Keys
	}
	if s.isVault() || (!s.Reachable && env.VaultPath != "") {
		s.Listing = true
		s.Listed, s.ListErr = vaultops.List(env)
	}
	return s
}

func (s MainState) isVault() bool { return s.Reachable && s.Backend.Mode == "vault" }

// open reports whether the agent can be asked to change its keys now.
func (s MainState) open() bool { return s.Reachable && !(s.isVault() && s.Backend.VaultLocked) }

// showsVault reports whether the list shows a vault's identities, rather
// than the keys of an agent that is not a vault.
func (s MainState) showsVault() bool {
	return s.Listing && s.ListErr == nil && s.Listed.Initialized
}

// Rows is what the list shows: a vault's identities, or the keys an agent
// that is not a vault holds, all of them loaded. Loaded keys come first, in
// the order they were listed, then the rest, which the window greys.
func (s MainState) Rows() []vaultops.IdentityView {
	if s.showsVault() {
		rows := slices.Clone(s.Listed.Identities)
		slices.SortStableFunc(rows, func(a, b vaultops.IdentityView) int {
			return cmp.Compare(loadRank(a.Loaded), loadRank(b.Loaded))
		})
		return rows
	}
	if !s.Reachable || s.isVault() {
		return nil
	}
	rows := make([]vaultops.IdentityView, 0, len(s.Keys))
	for _, k := range s.Keys {
		row := vaultops.IdentityView{Comment: k.Comment, KeyType: k.Format, Loaded: vaultops.LoadYes}
		if pub, err := ssh.ParsePublicKey(k.Blob); err == nil {
			row.Fingerprint = ssh.FingerprintSHA256(pub)
		}
		rows = append(rows, row)
	}
	return rows
}

// loadRank orders rows: loaded, then unknown, then not loaded.
func loadRank(l vaultops.LoadState) int {
	switch l {
	case vaultops.LoadYes:
		return 0
	case vaultops.LoadUnknown:
		return 1
	}
	return 2
}

// Dimmed reports whether the list greys a row: a key the agent is not
// serving, so ssh cannot use it now.
func Dimmed(id vaultops.IdentityView) bool { return id.Loaded != vaultops.LoadYes }

// MainActions says which of the window's buttons do anything.
type MainActions struct {
	Add, Remove, Load, Unload, Autoload bool
	Lock, Unlock                        bool
	Start, Stop                         bool
	// AutoloadOn is what the autoload button would set: the opposite of sel's.
	AutoloadOn bool
}

// mainActions works out MainActions for a state and the selected row, sel,
// or nil when none is selected. Every change goes through the running agent,
// so with none answering, or a locked vault, only Start or Unlock is offered.
// Remove, Load and Autoload are vault operations; an agent that is not a
// vault can only take a key out of memory.
func mainActions(s MainState, sel *vaultops.IdentityView) MainActions {
	vault := s.isVault()
	a := MainActions{
		Add:    s.open(),
		Lock:   vault && !s.Backend.VaultLocked,
		Unlock: vault && s.Backend.VaultLocked,
		Start:  s.Managed && !s.Reachable,
		Stop:   s.Managed && s.Reachable,
	}
	if sel == nil || !s.open() || sel.Fingerprint == "" {
		return a
	}
	a.Unload = sel.Loaded == vaultops.LoadYes
	if vault {
		a.Remove = true
		a.Load = sel.Loaded == vaultops.LoadNo
		a.Autoload = true
		a.AutoloadOn = !sel.Autoload
	}
	return a
}

// autoloadLabel names the autoload button by what pressing it does.
func autoloadLabel(on bool) string {
	if on {
		return "Turn autoload o&n"
	}
	return "Turn autoload o&ff"
}

// agentToggleLabel names the start/stop button by what pressing it does.
func agentToggleLabel(a MainActions) string {
	if a.Start {
		return "&Start agent"
	}
	return "S&top agent"
}

// keyRow renders one row in mainColumns order. inVault says whether
// autoload means anything for it.
func keyRow(id vaultops.IdentityView, inVault bool) []string {
	comment := id.Comment
	if comment == "" {
		comment = "(no comment)"
	}
	fp := id.Fingerprint
	if fp == "" {
		fp = "unparseable key"
	}
	autoload := "-"
	if inVault {
		autoload = "no"
		if id.Autoload {
			autoload = "yes"
		}
	}
	from := "-"
	if id.Filepath != "" {
		from = utils.DisplayPath(id.Filepath)
	}
	return []string{comment, id.KeyType, fp, id.Loaded.String(), autoload, from}
}

// sourceLine says where the listed keys come from.
func sourceLine(s MainState) string {
	switch {
	case s.showsVault() && s.Listed.VaultPath != "":
		return "Vault: " + utils.DisplayPath(s.Listed.VaultPath)
	case s.Reachable && !s.isVault():
		return "Keys in the agent's memory"
	}
	return ""
}

// mainStatus says what the window shows, or why it shows nothing, and
// whether that is a problem.
func mainStatus(s MainState) (text string, isErr bool) {
	locked := s.isVault() && s.Backend.VaultLocked
	switch {
	case locked && vaultops.CodeOf(s.ListErr) == vaultops.CodeVaultLocked:
		return "The vault is locked. Unlock it to see what it holds.", false
	case s.ListErr != nil:
		return describeLine(s.ListErr), true
	case s.Reachable && !s.isVault():
		return plural(len(s.Keys), "key") + " in the agent.", false
	case !s.Reachable && !s.showsVault():
		if s.Managed {
			return "The agent is not running. Start it to use your keys.", false
		}
		return "The agent is not reachable. It is not one sshush starts; check the program that provides it.", true
	case !s.Listed.Initialized:
		return "There is no vault yet. Create one with 'sshush vault init'.", false
	}
	n := len(s.Listed.Identities)
	if n == 1 {
		text = "1 identity in the vault"
	} else {
		text = fmt.Sprintf("%d identities in the vault", n)
	}
	switch {
	case !s.Reachable:
		return text + ". Start the agent to use or change them.", false
	case locked:
		return text + ". The vault is locked: unlock it to use or change them.", false
	}
	return text + ".", false
}

// describeLine is describe on one line, for the status line.
func describeLine(err error) string {
	if hint := vaultops.HintOf(err); hint != "" {
		return err.Error() + ". " + hint
	}
	return err.Error()
}
