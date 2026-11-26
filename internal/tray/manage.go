package tray

import (
	"cmp"
	"crypto/rsa"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
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

// Main window item IDs. Esc and the close button send idCancel, which
// closes it.
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
	idMainAutoload
	idMainSelftest
	idMainThemeLabel
	idMainTheme
	idMainRefresh
	idMainStateIcon
	idMainDetailTitle
	idMainDetailSub
	idMainFpLabel
	idMainFp
	idMainCopyFp
	idMainPubLabel
	idMainPub
	idMainCopyPub
	idMainFromLabel
	idMainFrom
	idMainRename
	idMainStartup
	idMainVersion
	idMainCreateKey
	idMainTabKeys
	idMainTabVault
	idMainTabSettings
	idVaultTitle
	idVaultSub
	idVaultOpenFolder
	idVaultCopyPath
	idVaultBackup
	idVaultImport
	idVaultMode
	idVaultPasswd
	idVaultRecovery
	idAutoLockTitle
	idAutoLockSession
	idAutoLockSleep
	idAutoLockIdleLabel
	idAutoLockIdle
	idAutoLockCaption
	// The Vault page's cards: a section header, then each card's title and
	// secondary line; the cards' buttons are above.
	idVaultSecHeader
	idVaultPasswdTitle
	idVaultPasswdCaption
	idVaultRecoveryTitle
	idVaultRecoveryCaption
	idVaultFileHeader
	idVaultFileTitle
	idVaultFileCaption
	idVaultModeHeader
	idVaultModeTitle
	idVaultModeCaption
	// The Vault page's facts take two IDs each, from here on; see
	// vaultRowLabel. Nothing may follow this in the list.
	idVaultRows
)

// vaultRowCount is how many facts the Vault page has room for.
const vaultRowCount = 8

// vaultRowLabel and vaultRowValue are the IDs of the Vault page's i-th fact.
func vaultRowLabel(i int) uint16 { return idVaultRows + uint16(2*i) }
func vaultRowValue(i int) uint16 { return idVaultRows + uint16(2*i+1) }

// Styles the main window adds to those in dialog_template.go.
const (
	wsMinimizeBox   = 0x00020000
	wsMaximizeBox   = 0x00010000
	wsThickFrame    = 0x00040000
	wsClipChildren  = 0x02000000
	wsBorder        = 0x00800000
	wsVScroll       = 0x00200000
	lvsReport       = 0x0001
	lvsSingleSel    = 0x0004
	lvsShowSelAlway = 0x0008
	lvsNoSortHeader = 0x8000
	ssIcon          = 0x0003
	ssCenterImage   = 0x0200
	ssNotify        = 0x0100
	ssEndEllipsis   = 0x4000
	ssPathEllipsis  = 0x8000
	cbsDropDownList = 0x0003
	esMultiline     = 0x0004
	esAutoVScroll   = 0x0040
	esReadOnly      = 0x0800
	bsAutoCheckbox  = 0x0003
	classListView   = "SysListView32"
	wsGroup         = 0x00020000
	bsAutoRadio     = 0x0009
	bsPushLike      = 0x1000
	classComboBox   = 0x85
	classListBox    = 0x83
	lbsNotify       = 0x0001
	lbsNoIntegral   = 0x0100
)

// mainColumns heads the key list; keyRow fills it in this order. The
// fingerprint, public key and file are in the details pane.
var mainColumns = []string{"Name", "Type", "Status"}

// buttonGlyphs gives each button its icon: a code point in Windows' icon font,
// Segoe Fluent Icons or, before Windows 11, Segoe MDL2 Assets, which share
// these. The lock, agent and load buttons change icon with what they do; see
// lockGlyph, agentGlyph and loadGlyph.
var buttonGlyphs = map[uint16]rune{
	idMainAdd:         0xE710, // Add
	idMainCreateKey:   0xE8D7, // Permissions: a key
	idMainRemove:      0xE74D, // Delete
	idMainAutoload:    0xE945, // LightningBolt
	idMainRename:      0xE8AC, // Rename
	idMainCopyFp:      0xE8C8, // Copy
	idMainCopyPub:     0xE8C8, // Copy
	idMainSelftest:    0xE9D9, // Diagnostic
	idMainRefresh:     0xE72C, // Refresh
	idVaultOpenFolder: 0xE838, // FolderOpen
	idVaultCopyPath:   0xE8C8, // Copy
	idVaultBackup:     0xE74E, // Save
	idVaultImport:     0xE8B5, // Import
	idVaultMode:       0xE8AB, // Switch
	idVaultPasswd:     0xE8D7, // Permissions: a key
	idVaultRecovery:   0xE895, // Sync
}

// hasGlyph reports whether the button id shows an icon.
func hasGlyph(id uint16) bool {
	_, ok := buttonGlyphs[id]
	return ok || id == idMainLock || id == idMainAgentToggle || id == idMainLoad
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

// loadGlyph is the load button's icon.
func loadGlyph(a MainActions) rune {
	if a.Unload {
		return 0xE898 // Upload: out of the agent
	}
	return 0xE896 // Download: into it
}

// mainTemplate creates the main window's controls. Where they go is
// mainLayout's business, so every item is placed at the origin; the order
// is the tab order, which follows the reading order: header,
// tabs, the Keys page's command bar, list and details, the Vault page,
// footer.
func mainTemplate() []uint16 {
	const stop = wsChild | wsVisible | wsTabStop
	btn := func(id uint16, text string) dlgItem {
		return dlgItem{style: stop, id: id, class: classButton, text: text}
	}
	label := func(id uint16, style uint32, text string) dlgItem {
		return dlgItem{style: wsChild | wsVisible | ssNoPrefix | style, id: id, class: classStatic, text: text}
	}
	field := func(id uint16, style uint32) dlgItem {
		return dlgItem{style: stop | esReadOnly | style, exStyle: wsExClientEdge, id: id, class: classEdit}
	}
	items := []dlgItem{
		// Header.
		label(idMainStateIcon, ssIcon|ssCenterImage, ""),
		label(idMainAgent, ssEndEllipsis, ""),
		label(idMainSource, ssPathEllipsis, ""),
		btn(idMainAgentToggle, "S&top agent"),
		// The page tabs: push-like radio buttons, which take the window's
		// theme, dark included, as a tab control does not.
		{style: stop | wsGroup | bsAutoRadio | bsPushLike, id: idMainTabKeys, class: classButton, text: "&Keys"},
		{style: stop | bsAutoRadio | bsPushLike, id: idMainTabVault, class: classButton, text: "&Vault"},
		{style: stop | bsAutoRadio | bsPushLike, id: idMainTabSettings, class: classButton, text: "Settin&gs"},
		// The Keys page's command bar. Add ends the tabs' group.
		{style: stop | wsGroup, id: idMainAdd, class: classButton, text: "&Add keys…"},
		btn(idMainCreateKey, "Create ke&y…"),
		btn(idMainRefresh, "Refresh"),
		// The list.
		{style: stop | wsBorder | lvsReport | lvsSingleSel | lvsShowSelAlway | lvsNoSortHeader, id: idMainList, className: classListView},
		// Details.
		label(idMainDetailTitle, ssEndEllipsis, ""),
		label(idMainDetailSub, ssEndEllipsis, ""),
		label(idMainFpLabel, 0, "Fingerprint"),
		field(idMainFp, esAutoHScroll),
		btn(idMainCopyFp, "Copy"),
		label(idMainPubLabel, 0, "Public key"),
		field(idMainPub, esMultiline|esAutoVScroll),
		btn(idMainCopyPub, "Co&py"),
		label(idMainFromLabel, 0, "Added from"),
		label(idMainFrom, ssPathEllipsis, ""),
		btn(idMainLoad, "L&oad"),
		btn(idMainAutoload, autoloadLabel(false)),
		btn(idMainRename, "Rena&me…"),
		btn(idMainRemove, "&Remove…"),
	}
	// The Vault page, hidden until its tab is chosen.
	hiddenBtn := func(id uint16, text string) dlgItem {
		return dlgItem{style: wsChild | wsTabStop, id: id, class: classButton, text: text}
	}
	hiddenLabel := func(id uint16, style uint32, text string) dlgItem {
		return dlgItem{style: wsChild | ssNoPrefix | style, id: id, class: classStatic, text: text}
	}
	items = append(items, hiddenLabel(idVaultTitle, ssEndEllipsis, ""), hiddenLabel(idVaultSub, 0, ""))
	for i := range vaultRowCount {
		items = append(items, hiddenLabel(vaultRowLabel(i), ssEndEllipsis, ""), hiddenLabel(vaultRowValue(i), ssPathEllipsis, ""))
	}
	// The Vault page's primary button, then its facts' buttons, then its
	// cards, in reading order.
	items = append(items,
		hiddenBtn(idMainLock, "Unlo&ck…"),
		hiddenBtn(idVaultOpenFolder, "Show in E&xplorer"),
		hiddenBtn(idVaultCopyPath, "Copy path"),
		hiddenLabel(idVaultSecHeader, ssEndEllipsis, "Security"),
		hiddenLabel(idVaultPasswdTitle, ssEndEllipsis, "Passphrase"),
		hiddenLabel(idVaultPasswdCaption, ssEndEllipsis, ""),
		hiddenBtn(idVaultPasswd, "Change…"),
		hiddenLabel(idVaultRecoveryTitle, ssEndEllipsis, "Recovery phrase"),
		hiddenLabel(idVaultRecoveryCaption, ssEndEllipsis, ""),
		hiddenBtn(idVaultRecovery, "Ne&w phrase…"),
		// Auto-lock.
		hiddenLabel(idAutoLockTitle, ssEndEllipsis, "Auto-lock"),
		hiddenLabel(idAutoLockCaption, ssEndEllipsis, ""),
		dlgItem{style: wsChild | wsTabStop | bsAutoCheckbox, id: idAutoLockSession, class: classButton, text: "Lock when Windows locks"},
		dlgItem{style: wsChild | wsTabStop | bsAutoCheckbox, id: idAutoLockSleep, class: classButton, text: "Lock when the PC sleeps"},
		hiddenLabel(idAutoLockIdleLabel, ssEndEllipsis, "Lock after no input"),
		dlgItem{style: wsChild | wsTabStop | wsVScroll | cbsDropDownList, id: idAutoLockIdle, class: classComboBox},
		hiddenLabel(idVaultFileHeader, ssEndEllipsis, "Backup"),
		hiddenLabel(idVaultFileTitle, ssEndEllipsis, "Vault file"),
		hiddenLabel(idVaultFileCaption, ssEndEllipsis, ""),
		hiddenBtn(idVaultBackup, "&Back up…"),
		hiddenBtn(idVaultImport, "Import…"),
		hiddenLabel(idVaultModeHeader, ssEndEllipsis, "Agent"),
		hiddenLabel(idVaultModeTitle, ssEndEllipsis, "What the agent serves"),
		hiddenLabel(idVaultModeCaption, ssEndEllipsis, ""),
		hiddenBtn(idVaultMode, modeLabel(false)),
	)
	items = append(items, settingsTemplate(hiddenBtn, hiddenLabel)...)
	items = append(items, []dlgItem{
		// Status and footer.
		label(idMainStatus, ssEndEllipsis, ""),
		btn(idMainSelftest, "Run s&elftest"),
		label(idMainVersion, ssEndEllipsis, ""),
	}...)
	style := uint32(wsPopup | wsCaption | wsSysMenu | wsMinimizeBox | wsMaximizeBox | wsThickFrame | wsClipChildren |
		dsSetFont | dsSetForeground | dsCenter)
	return buildTemplate(style, 520, 320, "sshush", "Segoe UI", 9, items)
}

// settingsTemplate is the Settings page's controls, hidden until its tab is
// chosen, in reading order: the left column, then the right.
func settingsTemplate(btn func(uint16, string) dlgItem, label func(uint16, uint32, string) dlgItem) []dlgItem {
	edit := func(id uint16) dlgItem {
		return dlgItem{style: wsChild | wsTabStop | esAutoHScroll, exStyle: wsExClientEdge, id: id, class: classEdit}
	}
	list := func(id uint16) dlgItem {
		return dlgItem{style: wsChild | wsTabStop | wsVScroll | wsBorder | lbsNotify | lbsNoIntegral, id: id, class: classListBox}
	}
	return []dlgItem{
		label(idSetAgentHeader, ssEndEllipsis, "Agent"),
		label(idSetTypeTitle, ssEndEllipsis, "The agent serves"),
		label(idSetTypeCaption, ssEndEllipsis, ""),
		{style: wsChild | wsTabStop | wsVScroll | cbsDropDownList, id: idSetType, class: classComboBox},
		label(idSetSocketTitle, ssEndEllipsis, "Agent pipe"),
		edit(idSetSocket),
		btn(idSetSocketDefault, "Use default"),
		label(idSetSocketCaption, ssEndEllipsis, ""),
		label(idSetVaultHeader, ssEndEllipsis, "Vault"),
		label(idSetVaultTitle, ssEndEllipsis, "Vault file"),
		edit(idSetVaultPath),
		btn(idSetVaultBrowse, "Browse…"),
		label(idSetVaultCaption, ssEndEllipsis, ""),
		label(idSetTrayHeader, ssEndEllipsis, "This app"),
		// The label's mnemonic moves the focus to the drop-down after it.
		{style: wsChild, id: idMainThemeLabel, class: classStatic, text: "T&heme"},
		{style: wsChild | wsTabStop | wsVScroll | cbsDropDownList, id: idMainTheme, class: classComboBox},
		{style: wsChild | wsTabStop | bsAutoCheckbox, id: idMainStartup, class: classButton, text: "Start at sign-&in"},
		label(idSetKeysHeader, ssEndEllipsis, "Key files"),
		label(idSetKeysCaption, ssEndEllipsis, ""),
		list(idSetKeysList),
		btn(idSetKeysAdd, "Add files…"),
		btn(idSetKeysRemove, "Remove"),
		label(idSetDirsHeader, ssEndEllipsis, "Search folders"),
		label(idSetDirsCaption, ssEndEllipsis, ""),
		list(idSetDirsList),
		btn(idSetDirsAdd, "Add folder…"),
		btn(idSetDirsRemove, "Remove"),
		btn(idSetDiscard, "Discard changes"),
		btn(idSetSave, "Save"),
	}
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
	// ConfiguredVault is the vault file config names, resolved; "" for none.
	ConfiguredVault string
	// VaultFile is the vault file listed when the agent does not serve it:
	// ConfiguredVault, or with none where the tray would create one.
	VaultFile string
	// ConfigIsVault reports that config has the agent serve a vault, which
	// is what it does once restarted, whatever the running one does now.
	ConfigIsVault bool
	// VaultAside reports that Listed is a vault the agent does not serve:
	// one read from its file while the agent serves key files, so it can be
	// created and looked after before the agent is switched to it. The
	// Keys page then shows the agent's keys, and only the Vault page the vault.
	VaultAside bool
	// FileFound reports that the listed vault's file is on this PC, and
	// FileSize and FileModTime are what it says about itself.
	FileFound   bool
	FileSize    int64
	FileModTime time.Time
}

// ProbeMain asks the agent what it is and what it holds, and lists the vault
// when there is one: the agent's own, or the configured file when no agent
// answers.
func ProbeMain(env vaultops.Env) MainState {
	s := MainState{Managed: !env.AgentUnmanaged}
	if snap := Probe(env.SocketPath); snap.Err == nil {
		s.Reachable, s.Backend, s.Keys = true, snap.Backend, snap.Keys
	}
	if env.VaultPath != "" {
		s.ConfiguredVault = vault.ResolveToFile(utils.ExpandHomeDirectory(env.VaultPath))
	}
	s.ConfigIsVault = env.AgentVaultPath != ""
	switch {
	case s.isVault() || (!s.Reachable && s.ConfigIsVault):
		s.Listing = true
		s.Listed, s.ListErr = vaultops.List(env)
	case env.AgentUnmanaged && env.VaultPath == "":
		// An agent sshush does not run, and no vault to look after.
	default:
		// The agent serves key files, or would: the vault is read from
		// its file, where it is or would be created.
		s.Listing, s.VaultAside = true, true
		venv := vaultEnv(env)
		s.VaultFile = vault.ResolveToFile(utils.ExpandHomeDirectory(venv.VaultPath))
		s.Listed, s.ListErr = vaultops.List(venv)
	}
	if s.hasVault() {
		if fi, err := os.Stat(s.Listed.VaultPath); err == nil && fi.Mode().IsRegular() {
			s.FileFound, s.FileSize, s.FileModTime = true, fi.Size(), fi.ModTime()
		}
	}
	return s
}

func (s MainState) isVault() bool { return s.Reachable && s.Backend.Mode == "vault" }

// needsVault reports that the vault this agent serves, or would, has not
// been created yet.
func (s MainState) needsVault() bool {
	return s.noVaultYet() && !s.VaultAside
}

// noVaultYet reports that there is no vault at the vault path yet, whether
// or not the agent serves it.
func (s MainState) noVaultYet() bool {
	return s.Listing && s.ListErr == nil && !s.Listed.Initialized
}

// hasVault reports that there is a vault to show on the Vault page, whether
// or not the agent serves it.
func (s MainState) hasVault() bool {
	return s.Listing && s.ListErr == nil && s.Listed.Initialized
}

// open reports whether the agent can be asked to change its keys now.
func (s MainState) open() bool { return s.Reachable && !(s.isVault() && s.Backend.VaultLocked) }

// showsVault reports whether the list shows a vault's identities, rather
// than the keys of an agent that is not a vault.
func (s MainState) showsVault() bool {
	return s.hasVault() && !s.VaultAside
}

// agentView is s as the Keys page sees it: without a vault the agent does
// not serve, whose state is the Vault page's business.
func (s MainState) agentView() MainState {
	if s.VaultAside {
		s.Listing, s.VaultAside, s.Listed, s.ListErr = false, false, vaultops.ListResult{}, nil
	}
	return s
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
		row := vaultops.IdentityView{Comment: k.Comment, KeyType: k.Format, Loaded: vaultops.LoadYes, PublicKey: k.Blob}
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
	Rename, Copy                        bool
	Lock, Unlock, CreateVault           bool
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
	inVault := s.isVault()
	a := MainActions{
		Add:         s.open() && !s.needsVault(),
		Lock:        inVault && !s.Backend.VaultLocked && !s.needsVault(),
		Unlock:      inVault && s.Backend.VaultLocked,
		CreateVault: s.needsVault(),
		Start:       s.Managed && !s.Reachable && !s.needsVault(), // a vault agent needs its vault
		Stop:        s.Managed && s.Reachable,
	}
	// Copying a public key needs nothing from the agent.
	a.Copy = sel != nil && len(sel.PublicKey) > 0
	if sel == nil || !s.open() || sel.Fingerprint == "" {
		return a
	}
	a.Unload = sel.Loaded == vaultops.LoadYes
	if inVault {
		a.Remove = true
		a.Load = sel.Loaded == vaultops.LoadNo
		a.Autoload = true
		a.AutoloadOn = !sel.Autoload
		a.Rename = true
	} else {
		// Renaming a key the agent holds rewrites its file, which only an
		// agent sshush runs can say where to find.
		a.Rename = s.Backend.SpeaksOps
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
	return []string{identityName(id), keyTypeLabel(id), statusLabel(id, inVault)}
}

// identityName names a key by its comment, else its type.
func identityName(id vaultops.IdentityView) string {
	if id.Comment != "" {
		return id.Comment
	}
	return keyTypeLabel(id) + " key"
}

// keyTypeLabel names a key's algorithm as people say it, with its size where
// that varies: "ED25519", "RSA 3072", "ECDSA P-256".
func keyTypeLabel(id vaultops.IdentityView) string {
	pub, err := ssh.ParsePublicKey(id.PublicKey)
	if err != nil {
		if id.KeyType == "" || id.KeyType == "?" {
			return "Unknown"
		}
		return id.KeyType
	}
	switch t := pub.Type(); t {
	case ssh.KeyAlgoED25519:
		return "ED25519"
	case ssh.KeyAlgoSKED25519:
		return "ED25519-SK"
	case ssh.KeyAlgoSKECDSA256:
		return "ECDSA-SK"
	case ssh.KeyAlgoECDSA256:
		return "ECDSA P-256"
	case ssh.KeyAlgoECDSA384:
		return "ECDSA P-384"
	case ssh.KeyAlgoECDSA521:
		return "ECDSA P-521"
	case ssh.KeyAlgoRSA:
		if c, ok := pub.(ssh.CryptoPublicKey); ok {
			if k, ok := c.CryptoPublicKey().(*rsa.PublicKey); ok {
				return fmt.Sprintf("RSA %d", k.N.BitLen())
			}
		}
		return "RSA"
	default:
		return t
	}
}

// statusLabel says whether ssh can use the key now and, in a vault, whether
// it will be loaded when the vault is unlocked.
func statusLabel(id vaultops.IdentityView, inVault bool) string {
	var s string
	switch id.Loaded {
	case vaultops.LoadYes:
		s = "Loaded"
	case vaultops.LoadNo:
		s = "Not loaded"
	default:
		s = "Unknown"
	}
	if inVault && id.Autoload {
		s += " · autoload"
	}
	return s
}

// DetailView is what the details pane shows for the selected key.
type DetailView struct {
	Title, Sub  string
	Fingerprint string
	// PublicKey is the key as a line of authorized_keys, comment included:
	// what a server or a forge wants pasted.
	PublicKey string
	From      string
	// Empty reports that nothing is selected, and the fields are hidden.
	Empty bool
}

// detailOf describes sel, or with nil the pane's empty state for s.
func detailOf(s MainState, sel *vaultops.IdentityView) DetailView {
	if sel == nil {
		d := DetailView{Empty: true, Title: "No key selected"}
		if len(s.Rows()) > 0 {
			d.Sub = "Select a key to see its fingerprint and public key."
		}
		return d
	}
	inVault := s.showsVault()
	d := DetailView{Title: identityName(*sel), Fingerprint: sel.Fingerprint, PublicKey: authorizedLine(*sel)}
	parts := []string{keyTypeLabel(*sel)}
	switch sel.Loaded {
	case vaultops.LoadYes:
		parts = append(parts, "loaded in the agent")
	case vaultops.LoadNo:
		parts = append(parts, "not loaded")
	}
	if inVault {
		if sel.Autoload {
			parts = append(parts, "loads when the vault is unlocked")
		} else {
			parts = append(parts, "loads only when you ask")
		}
	}
	d.Sub = strings.Join(parts, " · ")
	switch {
	case sel.Filepath != "":
		d.From = utils.DisplayPath(sel.Filepath)
	case inVault:
		d.From = "Not recorded"
	default:
		d.From = "Not known: the agent does not say"
	}
	if d.Fingerprint == "" {
		d.Fingerprint = "The key does not parse"
	}
	return d
}

// authorizedLine is id's public key as one authorized_keys line, or "".
func authorizedLine(id vaultops.IdentityView) string {
	pub, err := ssh.ParsePublicKey(id.PublicKey)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	if c := strings.TrimSpace(id.Comment); c != "" {
		line += " " + c
	}
	return line
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
	s = s.agentView()
	locked := s.isVault() && s.Backend.VaultLocked
	switch {
	case s.needsVault():
		return "There is no vault yet. Create one on the Vault tab to keep your keys encrypted in a single file.", false
	case locked && vaultops.CodeOf(s.ListErr) == vaultops.CodeVaultLocked:
		return "The vault is locked. Unlock it on the Vault tab to see what it holds.", false
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
		return "There is no vault yet. Create one on the Vault tab to keep your keys encrypted in a single file.", false
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
		return text + ". The vault is locked: unlock it on the Vault tab to use or change them.", false
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
