package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

type agentKeysMsg struct {
	keys    []*sshagent.Key
	err     error
	refresh bool
}

type agentStatusMsg struct {
	text  string
	isErr bool
}

type agentDaemonStateMsg struct {
	running bool
}

// daemonStartedForAddMsg reports the outcome of starting sshushd so a key the user
// picked while the agent was stopped can be added to it. err is nil when the daemon
// is up (including when it turned out to be running already).
type daemonStartedForAddMsg struct {
	path string
	err  error
}

// Seams over the daemon and agent calls the add-key flow makes; tests replace them.
var (
	startDaemon          = sshushd.StartDaemon
	daemonAlreadyRunning = sshushd.CheckAlreadyRunning
	addKeyToAgent        = addKeyToAgentCmd
)

// foundKeysMsg carries the result of a key search (see discoverKeysCmd).
type foundKeysMsg struct {
	paths []utils.KeyPath
	// rescan is true when the search was asked for (KeyRescan or a click), so
	// its result is reported in the status line; the search at startup is quiet.
	rescan bool
	// skipped lists configured [agent].search_dirs that could not be read.
	skipped []string
}

type agentLockResultMsg struct {
	err error
}

type agentUnlockResultMsg struct {
	err error
}

// vaultStatusMsg reports the live backend mode of the running agent and, for vault
// agents, whether the vault is locked (master key absent).
type vaultStatusMsg struct {
	mode      string // "vault" or "keys"
	reachable bool   // agent socket reachable and state readable
	locked    bool   // vault mode only: master key absent
}

// vaultPollMsg requests a periodic re-check of the vault lock state. gen identifies
// the poll chain; stale chains are dropped after daemon stop/start.
type vaultPollMsg struct {
	gen int
}

const (
	agentFocusTable = iota
	agentFocusFound
	agentFocusPassphrase
	agentFocusButtons // header daemon controls
)

// Indices into AgentScreen.buttons.Labels.
const (
	btnStart = iota
	btnStop
	btnReload
	btnLock
	btnUnlock
)

// AgentScreen is the agent tab: keys table, Start/Stop/Reload buttons, add/remove keys, lock/unlock.
type AgentScreen struct {
	sk            *Skeleton
	keyTable      KeyTable
	buttons       ButtonRow
	zonePrefix    string
	configPath    string
	socketPath    string
	status        string
	statusErr     bool
	daemonRunning bool
	width         int
	height        int

	foundKeys     []utils.KeyPath
	foundSelected int
	loadedFPs     map[string]bool

	// Scroll state, kept by syncScroll on each render: the first row drawn and
	// how many rows fit, for the Loaded Keys table and the Found Keys list.
	loadedOffset int
	loadedShown  int
	foundOffset  int
	foundShown   int

	fileSelector *FileSelector

	passInput  textinput.Model
	showPass   bool
	passAction string // "lock" or "unlock"

	commentOverlay commentOverlay

	vaultMode    string // live backend mode from the running agent ("vault"/"keys")
	vaultLocked  bool   // true when the running vault agent reports locked
	vaultKnown   bool   // true when the vault lock state has been read from a running agent
	vaultPollGen int    // generation of the active vault poll chain

	focus int
}

// NewAgentScreen creates an AgentScreen with the given skeleton, config path, and socket path.
func NewAgentScreen(sk *Skeleton, configPath, socketPath string) *AgentScreen {
	prefix := zone.NewPrefix()

	pi := textinput.New()
	pi.Placeholder = "passphrase"
	pi.EchoMode = textinput.EchoPassword
	pi.EchoCharacter = '*'

	btns := NewButtonRow("[s]tart", "[x]stop", "[r]eload", "[L]ock", "[u]nlock")
	btns.Focused = true
	btns.ZonePrefix = prefix + "ctrl-"

	kt := NewKeyTable(defaultViewWidth, defaultKeyTableHeight, sk.Styles())
	kt.ZonePrefix = prefix + "keys-"

	return &AgentScreen{
		sk:             sk,
		keyTable:       kt,
		buttons:        btns,
		zonePrefix:     prefix,
		configPath:     configPath,
		socketPath:     socketPath,
		status:         "loading...",
		loadedFPs:      make(map[string]bool),
		fileSelector:   NewFileSelector(ModeLoadFile, "Select key file", sk.Styles()),
		passInput:      pi,
		commentOverlay: newCommentOverlay(),
		focus:          agentFocusTable,
	}
}

func (s *AgentScreen) HasModal() bool {
	return s.fileSelector.Visible() || s.showPass || s.commentOverlay.active
}

func (s *AgentScreen) HasActiveTextInput() bool {
	// Passphrase uses HasModal; no other free-text fields on this screen.
	return false
}

func (s *AgentScreen) Init() tea.Cmd {
	return tea.Batch(
		fetchAgentKeysCmd(s.socketPath, false),
		checkDaemonCmd(s.socketPath),
		checkVaultStateCmd(s.socketPath),
		discoverKeysCmd(s.configPath, false),
	)
}

func (s *AgentScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if s.fileSelector.Visible() {
		switch msg.(type) {
		case tea.WindowSizeMsg, FileSelectedMsg, FilePickerCancelledMsg, agentKeysMsg, agentStatusMsg, agentDaemonStateMsg, agentLockResultMsg, agentUnlockResultMsg, vaultStatusMsg, vaultPollMsg, foundKeysMsg, ButtonFlashDoneMsg, daemonStartedForAddMsg:
			// Handle these below
		default:
			return s, s.fileSelector.Update(msg)
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.keyTable.SetSize(s.width, s.loadedKeysTableHeight(), s.sk.Styles())
		s.fileSelector.SetHeight(max(s.height-fileSelectorHeightReserve, fileSelectorMinHeight))
		return s, nil

	case ThemeChangedMsg:
		s.keyTable.SetSize(s.width, s.loadedKeysTableHeight(), s.sk.Styles())
		return s, nil

	case FileSelectedMsg:
		s.fileSelector.Hide()
		s.focus = agentFocusTable
		return s, s.addKeyCmd(msg.Path)

	case FilePickerCancelledMsg:
		s.fileSelector.Hide()
		s.focus = agentFocusTable
		return s, nil

	case agentDaemonStateMsg:
		s.daemonRunning = msg.running
		if s.sk != nil {
			state := "stopped"
			if msg.running {
				state = "running"
			}
			s.sk.UpdateWidgetValue(daemonStatusWidgetID(), state)
		}
		if msg.running {
			// Start a fresh vault poll chain; bumping the generation drops any stale chain.
			s.vaultPollGen++
			return s, pollVaultStateCmd(s.socketPath, s.vaultPollGen)
		}
		s.vaultKnown = false
		s.vaultLocked = false
		s.vaultPollGen++
		return s, func() tea.Msg {
			return VaultStateMsg{Mode: "", Locked: false, Known: false}
		}

	case ButtonFlashDoneMsg:
		s.buttons.ClearPress()
		return s, nil

	case daemonStartedForAddMsg:
		if msg.err != nil {
			s.status = "start failed: " + msg.err.Error()
			s.statusErr = true
			return s, tea.Batch(checkDaemonCmd(s.socketPath), checkVaultStateCmd(s.socketPath))
		}
		s.status = "started, adding key..."
		s.statusErr = false
		// Record the daemon as running exactly as a daemon state check would (footer
		// widget, fresh vault poll chain). The add's own status message then refreshes
		// keys, daemon state and vault state as after any add.
		_, stateCmd := s.Update(agentDaemonStateMsg{running: true})
		return s, tea.Batch(stateCmd, checkVaultStateCmd(s.socketPath), addKeyToAgent(s.socketPath, msg.path))

	case agentKeysMsg:
		if msg.err != nil {
			s.status = msg.err.Error()
			s.statusErr = true
			s.keyTable.SetRows(nil)
			s.keyTable.SetSize(s.width, s.loadedKeysTableHeight(), s.sk.Styles())
			s.loadedFPs = make(map[string]bool)
			s.syncTableSelection()
			return s, nil
		}
		rows := make([]table.Row, len(msg.keys))
		s.loadedFPs = make(map[string]bool)
		for i, k := range msg.keys {
			fp := ssh.FingerprintSHA256(k)
			rows[i] = table.Row{k.Type(), fp, k.Comment}
			s.loadedFPs[fp] = true
		}
		s.keyTable.SetRows(rows)
		s.keyTable.SetSize(s.width, s.loadedKeysTableHeight(), s.sk.Styles())
		s.syncTableSelection()
		s.statusErr = false
		if s.vaultKnown && s.vaultLocked {
			s.status = "vault locked - press u to unlock"
			s.statusErr = true
		} else if msg.refresh {
			s.status = fmt.Sprintf("refreshed %d key(s)", len(rows))
		} else if len(rows) == 0 {
			s.status = "no keys loaded"
		} else {
			s.status = fmt.Sprintf("%d key(s) loaded", len(rows))
		}
		s.clampFoundFocus()
		return s, nil

	case agentStatusMsg:
		s.status = msg.text
		s.statusErr = msg.isErr
		if s.sk != nil {
			s.sk.UpdateWidgetValue(daemonStatusWidgetID(), msg.text)
		}
		if !msg.isErr {
			return s, tea.Batch(
				fetchAgentKeysCmd(s.socketPath, false),
				checkDaemonCmd(s.socketPath),
				checkVaultStateCmd(s.socketPath),
			)
		}
		return s, tea.Batch(checkDaemonCmd(s.socketPath), checkVaultStateCmd(s.socketPath))

	case foundKeysMsg:
		s.foundKeys = msg.paths
		s.clampFoundFocus()
		if msg.rescan {
			s.status = rescanStatus(len(s.visibleFoundKeys()), msg.skipped)
			s.statusErr = false
		}
		return s, nil

	case agentLockResultMsg:
		s.showPass = false
		s.passInput.Blur()
		if msg.err != nil {
			s.status = "lock failed: " + msg.err.Error()
			s.statusErr = true
		} else {
			s.status = "agent locked"
			s.statusErr = false
			// Reset the cached lock state so the follow-up check refreshes it.
			s.vaultKnown = false
			s.vaultLocked = false
		}
		s.focus = agentFocusTable
		return s, tea.Batch(fetchAgentKeysCmd(s.socketPath, true), checkVaultStateCmd(s.socketPath))

	case agentUnlockResultMsg:
		s.showPass = false
		s.passInput.Blur()
		if msg.err != nil {
			s.status = "unlock failed: " + msg.err.Error()
			s.statusErr = true
		} else {
			s.status = "agent unlocked"
			s.statusErr = false
			// Reset the cached lock state so the follow-up check refreshes it.
			s.vaultKnown = false
			s.vaultLocked = false
		}
		s.focus = agentFocusTable
		return s, tea.Batch(fetchAgentKeysCmd(s.socketPath, true), checkVaultStateCmd(s.socketPath))

	case vaultStatusMsg:
		s.vaultMode = msg.mode
		if msg.reachable {
			s.vaultKnown = true
			s.vaultLocked = msg.locked
			if s.vaultLocked {
				s.status = "vault locked - press u to unlock"
				s.statusErr = true
			}
		} else {
			s.vaultKnown = false
			s.vaultLocked = false
		}
		return s, func() tea.Msg {
			return VaultStateMsg{Mode: s.vaultMode, Locked: s.vaultLocked, Known: s.vaultKnown}
		}

	case vaultPollMsg:
		if msg.gen != s.vaultPollGen || !s.daemonRunning {
			return s, nil
		}
		return s, tea.Batch(
			checkVaultStateCmd(s.socketPath),
			pollVaultStateCmd(s.socketPath, msg.gen),
		)

	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft || s.fileSelector.Visible() || s.showPass || s.commentOverlay.active {
			return s, nil
		}
		return s.handleMouse(msg.X, msg.Y, false)

	case tea.MouseReleaseMsg:
		if msg.Button != tea.MouseLeft || s.fileSelector.Visible() || s.showPass || s.commentOverlay.active {
			return s, nil
		}
		return s.handleMouse(msg.X, msg.Y, true)

	case commentOverlaySavedMsg:
		s.commentOverlay.Hide()
		if msg.err != nil {
			s.status = msg.err.Error()
			s.statusErr = true
			return s, nil
		}
		s.status = "comment updated"
		s.statusErr = false
		return s, fetchAgentKeysCmd(s.socketPath, true)

	case tea.KeyPressMsg:
		if s.commentOverlay.active {
			return s, s.commentOverlay.Update(msg, s.socketPath, s.configPath)
		}
		if s.showPass {
			return s.handlePassInput(msg)
		}
		if s.fileSelector.Visible() {
			return s, s.fileSelector.Update(msg)
		}
		return s.handleKeys(msg)
	}

	if s.focus == agentFocusTable {
		cmd := s.keyTable.Update(msg)
		s.syncTableSelection()
		return s, cmd
	}
	return s, nil
}

func (s *AgentScreen) handleKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if KeyMatches(msg, KeyRescan) {
		return s, s.rescanKeys()
	}
	switch msg.String() {
	case "q", "esc":
		return s, tea.Quit

	case "up", "k":
		switch s.focus {
		case agentFocusTable:
			if s.keyTable.Table.Cursor() > 0 {
				cmd := s.keyTable.Update(msg)
				s.syncTableSelection()
				return s, cmd
			}
			s.syncTableSelection()
			return s, navToTabBarCmd()
		case agentFocusFound:
			if s.foundSelected > 0 {
				s.foundSelected--
			} else {
				s.focusFirstLoadedKey()
			}
		}
		return s, nil

	case "down", "j":
		switch s.focus {
		case agentFocusTable:
			rows := s.keyTable.Table.Rows()
			cursor := s.keyTable.Table.Cursor()
			if len(rows) > 0 && cursor < len(rows)-1 {
				cmd := s.keyTable.Update(msg)
				s.syncTableSelection()
				return s, cmd
			}
			visible := s.visibleFoundKeys()
			if len(visible) > 0 {
				s.focus = agentFocusFound
				s.foundSelected = 0
				s.syncTableSelection()
			}
		case agentFocusFound:
			visible := s.visibleFoundKeys()
			maxIdx := s.foundKeysMaxIndex(visible)
			if s.foundSelected < maxIdx {
				s.foundSelected++
			}
		}
		return s, nil

	case "enter":
		if s.focus == agentFocusFound {
			return s.addFoundKey()
		}
		return s, nil

	case "s":
		return s.pressButton(0) // Start
	case "x":
		return s.pressButton(1) // Stop
	case "r":
		return s.pressButton(2) // Reload

	case "a":
		if s.focus == agentFocusFound {
			return s.addFoundKey()
		}
		return s, s.fileSelector.Show()

	case "backspace", "delete", "d":
		if s.focus == agentFocusTable {
			return s.removeSelectedKey()
		}
		return s, nil

	case "e":
		if s.focus == agentFocusTable {
			return s.editSelectedKeyComment()
		}
		return s, nil

	case "L":
		return s, s.startLock()

	case "u":
		return s, s.startPassphrase("unlock")
	}

	if s.focus == agentFocusTable {
		cmd := s.keyTable.Update(msg)
		s.syncTableSelection()
		return s, cmd
	}
	return s, nil
}

func (s *AgentScreen) handlePassInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.showPass = false
		s.passInput.Blur()
		s.focus = agentFocusTable
		return s, nil
	case "enter":
		passphrase := s.passInput.Value()
		if s.passAction == "lock" {
			return s, lockAgentCmd(s.socketPath, passphrase)
		}
		return s, unlockAgentCmd(s.socketPath, passphrase)
	}
	var cmd tea.Cmd
	s.passInput, cmd = s.passInput.Update(msg)
	return s, cmd
}

// handleMouse handles a left click (release false) or its release (release
// true); both arrive for one click. The re-scan control acts on the release
// only, so one click searches once.
func (s *AgentScreen) handleMouse(x, y int, release bool) (tea.Model, tea.Cmd) {
	if inZoneBounds(s.rescanZoneID(), x, y) {
		if !release {
			return s, nil
		}
		return s, s.rescanKeys()
	}
	// Only rows on screen are clickable; zones of rows scrolled away are stale.
	rows := s.keyTable.Table.Rows()
	for i := s.loadedOffset; i < min(s.loadedOffset+s.loadedShown, len(rows)); i++ {
		if inZoneBounds(fmt.Sprintf("%skey-%d", s.zonePrefix, i), x, y) {
			s.focus = agentFocusTable
			s.keyTable.Table.SetCursor(i)
			s.syncTableSelection()
			return s, nil
		}
	}
	visible := s.visibleFoundKeys()
	for i := s.foundOffset; i < min(s.foundOffset+s.foundShown, len(visible)); i++ {
		if inZoneBounds(fmt.Sprintf("%sfound-%d", s.zonePrefix, i), x, y) {
			s.focus = agentFocusFound
			s.foundSelected = i
			return s.addFoundKey()
		}
	}
	return s, nil
}

func (s *AgentScreen) pressButton(btn int) (tea.Model, tea.Cmd) {
	s.buttons.Active = btn
	switch btn {
	case btnLock:
		return s, s.startLock()
	case btnUnlock:
		return s, s.startPassphrase("unlock")
	}
	s.buttons.Press()
	s.statusErr = false

	var action tea.Cmd
	switch btn {
	case btnStart: // Start
		s.status = "starting..."
		action = startDaemonCmd(s.configPath, s.socketPath)
	case btnStop: // Stop
		s.status = "stopping..."
		action = stopDaemonCmd()
	case btnReload: // Reload
		s.status = "reloading..."
		action = reloadDaemonCmd(s.configPath, s.socketPath)
	}
	return s, tea.Batch(action, ButtonFlashCmd())
}

// startPassphrase opens the lock/unlock passphrase prompt for the given action ("lock"/"unlock").
func (s *AgentScreen) startPassphrase(action string) tea.Cmd {
	s.showPass = true
	s.passAction = action
	s.passInput.SetValue("")
	if action == "lock" {
		s.passInput.Placeholder = "lock passphrase"
	} else {
		s.passInput.Placeholder = "unlock passphrase"
	}
	s.focus = agentFocusPassphrase
	return s.passInput.Focus()
}

// startLock begins locking the agent. A live vault agent locks without a passphrase:
// the daemon wipes the in-memory master key, so the global unlock key already held is
// simply discarded. Keys-mode agents need a passphrase (it becomes the unlock key), so
// the passphrase prompt is shown instead.
func (s *AgentScreen) startLock() tea.Cmd {
	if s.vaultMode == "vault" {
		s.statusErr = false
		s.status = "locking..."
		return lockVaultCmd(s.socketPath)
	}
	return s.startPassphrase("lock")
}

// removeSelectedKey removes the selected key from the running agent. For a vault-backed
// agent this only hides the key from the current session (LOADED becomes "no" in the
// Vault tab); it does not delete the identity from the vault. Permanent deletion is only
// available from the Vault tab's own remove action. Keys-mode agents have no persistent
// storage to preserve, so removal there is the plain ssh-agent forget.
func (s *AgentScreen) removeSelectedKey() (tea.Model, tea.Cmd) {
	row := s.keyTable.SelectedRow()
	if row == nil {
		return s, nil
	}
	fp := row[1]
	if s.vaultMode == "vault" {
		return s, sessionUnloadVaultKeyCmd(s.socketPath, fp)
	}
	return s, removeKeyFromAgentCmd(s.socketPath, fp)
}

// editSelectedKeyComment opens the comment overlay for the currently selected key row.
func (s *AgentScreen) editSelectedKeyComment() (tea.Model, tea.Cmd) {
	row := s.keyTable.SelectedRow()
	if row == nil {
		return s, nil
	}
	keyType, fp, comment := row[0], row[1], row[2]
	return s, s.commentOverlay.Show(fp, keyType, comment)
}

func (s *AgentScreen) addFoundKey() (tea.Model, tea.Cmd) {
	visible := s.visibleFoundKeys()
	if s.foundSelected >= len(visible) {
		return s, nil
	}
	kp := visible[s.foundSelected]
	return s, s.addKeyCmd(kp.Path)
}

// addKeyCmd adds the key file at path to the agent. When the daemon is not running
// it is started first (the same start as the Start control), then the key is added
// once daemonStartedForAddMsg reports it up.
func (s *AgentScreen) addKeyCmd(path string) tea.Cmd {
	if s.daemonRunning {
		return addKeyToAgent(s.socketPath, path)
	}
	s.status = "starting agent..."
	s.statusErr = false
	return startDaemonThenAddCmd(s.configPath, s.socketPath, path)
}

// rescanKeys searches ~/.ssh, the current directory and the configured
// [agent].search_dirs again for keys to offer under Found Keys. The config is
// read afresh, so search_dirs edited while the TUI runs take effect.
func (s *AgentScreen) rescanKeys() tea.Cmd {
	s.status = "searching for keys..."
	s.statusErr = false
	return discoverKeysCmd(s.configPath, true)
}

// rescanZoneID is the bubblezone id of the clickable re-scan control.
func (s *AgentScreen) rescanZoneID() string { return s.zonePrefix + "rescan" }

// clampFoundFocus keeps the Found Keys selection inside the visible list, and
// moves focus back to the loaded keys when that list has emptied.
func (s *AgentScreen) clampFoundFocus() {
	if s.focus != agentFocusFound {
		return
	}
	visible := s.visibleFoundKeys()
	if len(visible) == 0 {
		s.focusFirstLoadedKey()
		s.foundSelected = 0
	} else if s.foundSelected > s.foundKeysMaxIndex(visible) {
		s.foundSelected = s.foundKeysMaxIndex(visible)
	}
}

// rescanStatus is the status line after a re-scan: how many keys not already
// in the agent were found, and which configured directories were skipped.
func rescanStatus(found int, skipped []string) string {
	var text string
	switch found {
	case 0:
		text = "no new keys found"
	case 1:
		text = "found 1 new key"
	default:
		text = fmt.Sprintf("found %d new keys", found)
	}
	if len(skipped) > 0 {
		display := make([]string, len(skipped))
		for i, d := range skipped {
			display[i] = utils.DisplayPath(d)
		}
		text += " (skipped unreadable: " + strings.Join(display, ", ") + ")"
	}
	return text
}

func (s *AgentScreen) focusFirstLoadedKey() {
	s.focus = agentFocusTable
	if rows := s.keyTable.Table.Rows(); len(rows) > 0 {
		s.keyTable.Table.SetCursor(0)
	}
	s.syncTableSelection()
}

// syncTableSelection keeps row data visible and toggles row highlight only while
// the loaded-keys list is the active focus target.
func (s *AgentScreen) syncTableSelection() {
	if s.sk == nil {
		return
	}
	rows := s.keyTable.Table.Rows()
	if len(rows) > 0 && s.keyTable.Table.Cursor() < 0 {
		s.keyTable.Table.SetCursor(0)
	}
	highlighted := s.sk.ScreenActive() && s.focus == agentFocusTable && len(rows) > 0
	s.keyTable.SetSelectionHighlighted(highlighted, s.sk.Styles())
}

func (s *AgentScreen) loadedKeysTableHeight() int {
	rowCount := len(s.keyTable.Table.Rows())
	if rowCount == 0 {
		return minTableHeight
	}
	// Table view = header row + header rule + data rows.
	h := rowCount + 2
	if h > agentLoadedKeysMaxRows {
		h = agentLoadedKeysMaxRows
	}
	if h < minTableHeight {
		h = minTableHeight
	}
	return h
}

// foundKeysMaxIndex is the last selectable Found Keys row. Every found key can
// be selected; rows past the bottom of the list scroll into view.
func (s *AgentScreen) foundKeysMaxIndex(visible []utils.KeyPath) int {
	return max(len(visible)-1, 0)
}

// loadedKeysRows is how many key rows the Loaded Keys table shows at once: its
// height less the header row and rule.
func (s *AgentScreen) loadedKeysRows() int {
	return max(s.loadedKeysTableHeight()-2, 1)
}

// foundKeysRows is how many Found Keys rows to draw for total found keys: all
// of them if they fit under Loaded Keys, otherwise what fits (keeping a line
// for the scroll hint), and never fewer than foundKeysMinVisible.
func (s *AgentScreen) foundKeysRows(total int) int {
	h := s.height
	if h < 1 {
		h = defaultViewHeight
	}
	// Loaded Keys: title, border top and bottom, table; plus its scroll hint.
	used := 1 + 2 + s.loadedKeysTableHeight()
	if len(s.keyTable.Table.Rows()) > s.loadedKeysRows() {
		used++
	}
	// The blank line between sections, then the Found Keys title and border.
	avail := h - used - 1 - 3
	if total <= max(avail, foundKeysMinVisible) {
		return total
	}
	return min(max(avail-1, foundKeysMinVisible), total)
}

// syncScroll keeps the selected row of each list inside its scroll window. It
// runs on every render, after keys, focus or the window size have changed.
func (s *AgentScreen) syncScroll() {
	rows := len(s.keyTable.Table.Rows())
	s.loadedShown = min(s.loadedKeysRows(), rows)
	s.loadedOffset = scrollOffset(s.loadedOffset, s.keyTable.Table.Cursor(), rows, s.loadedShown)

	total := len(s.visibleFoundKeys())
	s.foundShown = s.foundKeysRows(total)
	s.foundOffset = scrollOffset(s.foundOffset, s.foundSelected, total, s.foundShown)
}

func sectionBoxWidth(width int) int {
	return SectionWidth(width)
}

func (s *AgentScreen) visibleFoundKeys() []utils.KeyPath {
	var visible []utils.KeyPath
	for _, p := range s.foundKeys {
		pubKey, _, _, err := agent.ParseKeyFromPath(p.Path)
		if err != nil {
			visible = append(visible, p)
			continue
		}
		fp := ssh.FingerprintSHA256(pubKey)
		if !s.loadedFPs[fp] {
			visible = append(visible, p)
		}
	}
	return visible
}

func (s *AgentScreen) View() tea.View {
	width := s.width
	height := s.height
	if width < 1 {
		width = defaultViewWidth
	}
	if height < 1 {
		height = defaultViewHeight
	}
	active := s.sk.ScreenActive()
	if s.fileSelector.Visible() {
		innerW := width - 2
		if innerW < 1 {
			innerW = 1
		}
		return tea.NewView(lipgloss.Place(innerW, height, lipgloss.Center, lipgloss.Center,
			s.fileSelector.View(width, height, active, s.sk.Styles())))
	}

	if s.showPass {
		st := s.sk.Styles()
		title := st.SectionTitleStyle.Render("Enter " + s.passAction + " passphrase")
		return tea.NewView(lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			title+"\n"+st.FocusedBorderStyle.Render(s.passInput.View())))
	}

	if s.commentOverlay.active {
		innerW := width - 2
		if innerW < 1 {
			innerW = 1
		}
		return tea.NewView(lipgloss.Place(innerW, height, lipgloss.Center, lipgloss.Center,
			s.commentOverlay.View(s.sk.Styles(), sectionBoxWidth(width))))
	}

	w := width
	if w < 1 {
		w = defaultViewWidth
	}

	s.syncScroll()

	var sections []string
	sections = append(sections, s.renderLoadedKeys(w, active))

	visible := s.visibleFoundKeys()
	sections = append(sections, "")
	if len(visible) > 0 {
		foundContent := s.renderFoundKeys(visible, w, active)
		sections = append(sections, foundContent)
	} else {
		sections = append(sections, lipgloss.Place(w, 0, lipgloss.Center, lipgloss.Top,
			s.sk.Styles().DimStyle.Render("No other keys found ")+s.renderRescanControl()))
	}

	content := strings.Join(sections, "\n")
	contentLines := strings.Count(content, "\n") + 1
	if padTop := (height - contentLines) / 2; padTop > 0 {
		content = strings.Repeat("\n", padTop) + content
	}
	return tea.NewView(content)
}

func (s *AgentScreen) StatusText() string {
	st := s.sk.Styles()
	statusStyle := st.AccentStyle
	if s.statusErr {
		statusStyle = st.ErrorStyle
	}
	return statusStyle.Render(s.status)
}

func (s *AgentScreen) StatusTextRaw() (string, bool) {
	return s.status, s.statusErr
}

// ControlButtonsInlineView returns buttons as styled text without per-button borders,
// for use inside a single daemon box (same headerTabBorder as tabs).
func (s *AgentScreen) ControlButtonsInlineView(focused bool) string {
	st := s.sk.Styles()
	var parts []string
	for i, label := range s.buttons.Labels {
		var style lipgloss.Style
		switch {
		case s.buttons.Pressed == i:
			style = st.FocusedButtonStyle
		case s.buttons.Active == i && focused:
			style = st.FocusedButtonStyle
		case s.buttons.Active == i:
			style = st.ButtonActiveStyle
		default:
			style = st.UnfocusedButtonStyle
		}
		rendered := style.Render(label)
		if s.buttons.ZonePrefix != "" {
			rendered = zone.Mark(s.buttons.ZonePrefix+label, rendered)
		}
		parts = append(parts, rendered)
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, parts...)
}

func (s *AgentScreen) renderLoadedKeys(width int, active bool) string {
	s.syncTableSelection()

	st := s.sk.Styles()
	title := st.SectionTitleStyle.Render(" Loaded Keys")

	focused := active && s.focus == agentFocusTable && len(s.keyTable.Table.Rows()) > 0
	border := st.UnfocusedBorderStyle
	if s.vaultKnown && s.vaultLocked {
		border = st.WarnBorderStyle
	} else if focused {
		border = st.FocusedBorderStyle
	}

	table := s.keyTable.AgentViewWindow(s.zonePrefix, focused, st, s.loadedOffset, s.loadedShown)
	if rows := len(s.keyTable.Table.Rows()); rows > s.loadedShown {
		table += "\n" + st.DimStyle.Render(" "+scrollHint(s.loadedOffset, s.loadedShown, rows))
	}
	content := border.Render(table)
	return lipgloss.Place(width, 0, lipgloss.Center, lipgloss.Top, title+"\n"+content)
}

func (s *AgentScreen) renderFoundKeys(visible []utils.KeyPath, width int, active bool) string {
	st := s.sk.Styles()
	title := st.SectionTitleStyle.Render(" Found Keys") + " " + s.renderRescanControl()
	var lines []string
	end := min(s.foundOffset+s.foundShown, len(visible))
	for i := s.foundOffset; i < end; i++ {
		linePrefix := "  "
		selected := active && s.focus == agentFocusFound && i == s.foundSelected
		if selected {
			linePrefix = "> "
		}

		var line string
		if selected {
			style := lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color(s.sk.Theme().Focus)).Bold(true)
			text := utils.DisplayPath(visible[i].Path)
			if visible[i].IsSymlink {
				text += " -> " + utils.DisplayPath(visible[i].RealPath)
			}
			line = style.Render(linePrefix + text)
		} else if visible[i].IsSymlink {
			name := st.FocusStyle.Render(utils.DisplayPath(visible[i].Path))
			target := st.DimStyle.Render(" -> " + utils.DisplayPath(visible[i].RealPath))
			line = st.AccentStyle.Render(linePrefix) + name + target
		} else {
			line = st.AccentStyle.Render(linePrefix + utils.DisplayPath(visible[i].Path))
		}
		rendered := zone.Mark(fmt.Sprintf("%sfound-%d", s.zonePrefix, i), line)
		lines = append(lines, rendered)
	}
	if len(visible) > s.foundShown {
		lines = append(lines, st.DimStyle.Render(" "+scrollHint(s.foundOffset, s.foundShown, len(visible))))
	}
	content := strings.Join(lines, "\n")
	boxW := sectionBoxWidth(width)
	border := st.UnfocusedBorderStyle
	if active && s.focus == agentFocusFound {
		border = st.FocusedBorderStyle
	}
	return lipgloss.Place(width, 0, lipgloss.Center, lipgloss.Top,
		title+"\n"+border.Width(boxW-4).Render(content))
}

// renderRescanControl renders the clickable "[f] search again" control shown
// beside Found Keys (or in its place when nothing was found).
func (s *AgentScreen) renderRescanControl() string {
	label := "[" + KeyRescan[0] + "] search again"
	return zone.Mark(s.rescanZoneID(), s.sk.Styles().AccentStyle.Render(label))
}

func (s *AgentScreen) HelpEntries() []string {
	st := s.sk.Styles()
	return []string{
		st.HelpRow("Agent controls", ""),
		st.HelpRow("a", "Add key"),
		st.HelpRow(KeyRescan[0], "Search again for keys"),
		st.HelpRow("e", "Edit comment"),
		st.HelpRow("d / bksp", "Remove key"),
		st.HelpRow("L", "Lock vault"),
		st.HelpRow("u", "Unlock vault"),
		"",
	}
}

// Commands

func fetchAgentKeysCmd(socketPath string, refresh bool) tea.Cmd {
	return func() tea.Msg {
		if socketPath == "" {
			return agentKeysMsg{err: fmt.Errorf("no socket path configured")}
		}
		session, err := agent.Open(socketPath)
		if err != nil {
			return agentKeysMsg{err: fmt.Errorf("agent not running")}
		}
		defer session.Close()
		keys, err := session.List()
		if err != nil {
			return agentKeysMsg{err: fmt.Errorf("agent not running")}
		}
		return agentKeysMsg{keys: keys, refresh: refresh}
	}
}

func checkDaemonCmd(socketPath string) tea.Cmd {
	return func() tea.Msg {
		return agentDaemonStateMsg{running: sshushd.CheckAlreadyRunning(socketPath)}
	}
}

func startDaemonCmd(configPath, socketPath string) tea.Cmd {
	return func() tea.Msg {
		if err := sshushd.StartDaemon(configPath, socketPath); err != nil {
			if err.Error() == "already running" {
				return agentStatusMsg{text: "already running"}
			}
			return agentStatusMsg{text: err.Error(), isErr: true}
		}
		return agentStatusMsg{text: "started"}
	}
}

// startDaemonThenAddCmd starts sshushd and reports back with daemonStartedForAddMsg so
// the key at path can be added. A daemon that is already up (the cached state was
// stale) counts as started.
func startDaemonThenAddCmd(configPath, socketPath, path string) tea.Cmd {
	return func() tea.Msg {
		if daemonAlreadyRunning(socketPath) {
			return daemonStartedForAddMsg{path: path}
		}
		return daemonStartedForAddMsg{path: path, err: startDaemon(configPath, socketPath)}
	}
}

func stopDaemonCmd() tea.Cmd {
	return func() tea.Msg {
		pidFilePath := runtime.PidFilePath()
		if _, err := os.Stat(pidFilePath); os.IsNotExist(err) {
			return agentStatusMsg{text: "agent not running", isErr: true}
		}
		if err := sshushd.StopDaemon(pidFilePath); err != nil {
			return agentStatusMsg{text: "stop failed", isErr: true}
		}
		return agentStatusMsg{text: "stopped"}
	}
}

func reloadDaemonCmd(configPath, socketPath string) tea.Cmd {
	return func() tea.Msg {
		pidFilePath := runtime.PidFilePath()
		if err := sshushd.ReloadDaemon(configPath, socketPath, pidFilePath); err != nil {
			return agentStatusMsg{text: err.Error(), isErr: true}
		}
		return agentStatusMsg{text: "reloaded"}
	}
}

func removeKeyFromAgentCmd(socketPath, fingerprint string) tea.Cmd {
	return func() tea.Msg {
		session, err := agent.Open(socketPath)
		if err != nil {
			return agentStatusMsg{text: "agent not running", isErr: true}
		}
		defer session.Close()
		removed, err := session.RemoveByFingerprint(fingerprint)
		if err != nil {
			return agentStatusMsg{text: "agent not running", isErr: true}
		}
		if !removed {
			return agentStatusMsg{text: "key not found", isErr: true}
		}
		return agentStatusMsg{text: "key removed"}
	}
}

// sessionUnloadVaultKeyCmd hides a vault identity from the current agent session
// (LOADED -> no) without deleting it from the vault or changing its persisted
// autoload flag. Used by the Agent tab's remove key, since a vault-backed agent's
// plain ssh-agent Remove permanently deletes the identity (that behavior is reserved
// for the Vault tab's own remove action, matching sshush vault remove).
func sessionUnloadVaultKeyCmd(socketPath, fingerprint string) tea.Cmd {
	return func() tea.Msg {
		session, err := agent.Open(socketPath)
		if err != nil {
			return agentStatusMsg{text: "unload failed: " + err.Error(), isErr: true}
		}
		defer session.Close()
		if err := vault.SessionUnload(session, fingerprint); err != nil {
			return agentStatusMsg{text: "unload failed: " + err.Error(), isErr: true}
		}
		return agentStatusMsg{text: "key unloaded from session"}
	}
}

func addKeyToAgentCmd(socketPath, path string) tea.Cmd {
	return func() tea.Msg {
		session, err := agent.Open(socketPath)
		if err != nil {
			return agentStatusMsg{text: "add failed: " + err.Error(), isErr: true}
		}
		defer session.Close()
		// Default autoload on for vault (same as CLI without --no-autoload).
		backend, backendErr := session.Backend()
		if backendErr == nil && backend.Mode == "vault" {
			err = vault.AddPrivateKeyFile(session, path, true)
		} else {
			err = session.AddKeyFromPath(path)
		}
		if err != nil {
			return agentStatusMsg{text: "add failed: " + err.Error(), isErr: true}
		}
		return agentStatusMsg{text: "key added: " + utils.DisplayPath(path)}
	}
}

func lockAgentCmd(socketPath, passphrase string) tea.Cmd {
	return func() tea.Msg {
		session, err := agent.Open(socketPath)
		if err != nil {
			return agentLockResultMsg{err: err}
		}
		defer session.Close()
		return agentLockResultMsg{err: session.Lock([]byte(passphrase))}
	}
}

// lockVaultCmd locks the vault agent immediately. Locking needs no passphrase: the
// daemon wipes the in-memory master key.
func lockVaultCmd(socketPath string) tea.Cmd {
	return func() tea.Msg {
		session, err := agent.Open(socketPath)
		if err != nil {
			return agentLockResultMsg{err: err}
		}
		defer session.Close()
		return agentLockResultMsg{err: session.Lock(nil)}
	}
}

func unlockAgentCmd(socketPath, passphrase string) tea.Cmd {
	return func() tea.Msg {
		session, err := agent.Open(socketPath)
		if err != nil {
			return agentUnlockResultMsg{err: err}
		}
		defer session.Close()
		return agentUnlockResultMsg{err: session.Unlock([]byte(passphrase))}
	}
}

// checkVaultState queries the running agent for its backend mode and, for vault agents,
// whether the vault is locked.
func checkVaultState(socketPath string) vaultStatusMsg {
	session, err := agent.Open(socketPath)
	if err != nil {
		return vaultStatusMsg{}
	}
	defer session.Close()
	backend, err := session.Backend()
	if err != nil {
		return vaultStatusMsg{}
	}
	return vaultStatusMsg{mode: backend.Mode, reachable: true, locked: backend.VaultLocked}
}

func checkVaultStateCmd(socketPath string) tea.Cmd {
	return func() tea.Msg {
		return checkVaultState(socketPath)
	}
}

// pollVaultStateCmd schedules the next periodic vault state check. Only the poll chain
// matching the current generation is kept; daemon stop/start bumps the generation to
// drop stale chains.
func pollVaultStateCmd(socketPath string, gen int) tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return vaultPollMsg{gen: gen}
	})
}

// discoverKeysCmd searches ~/.ssh, the current directory and the
// [agent].search_dirs of the config at configPath for private keys. A config
// that is missing or does not load adds no directories; configured directories
// that cannot be read are skipped and reported in foundKeysMsg.skipped.
func discoverKeysCmd(configPath string, rescan bool) tea.Cmd {
	return func() tea.Msg {
		dirs := configuredSearchDirs(configPath)
		return foundKeysMsg{
			paths:   utils.DiscoverKeyPaths(dirs, true, true, false),
			rescan:  rescan,
			skipped: utils.UnreadableDirs(dirs),
		}
	}
}

// configuredSearchDirs returns [agent].search_dirs from the config at
// configPath, or nil when there is no config or it does not load.
func configuredSearchDirs(configPath string) []string {
	if configPath == "" {
		return nil
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil
	}
	return cfg.SearchDirs
}
