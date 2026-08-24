package tui

import (
	"fmt"
	"image/color"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/theme"
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

type foundKeysMsg struct {
	paths []string
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
	agentFocusButtons = iota
	agentFocusTable
	agentFocusFound
	agentFocusPassphrase
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

	foundKeys     []string
	foundSelected int
	loadedFPs     map[string]bool

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

func (s *AgentScreen) Init() tea.Cmd {
	return tea.Batch(
		fetchAgentKeysCmd(s.socketPath, false),
		checkDaemonCmd(s.socketPath),
		checkVaultStateCmd(s.socketPath),
		discoverKeysCmd(),
	)
}

func (s *AgentScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if s.fileSelector.Visible() {
		switch msg.(type) {
		case tea.WindowSizeMsg, FileSelectedMsg, FilePickerCancelledMsg, agentKeysMsg, agentStatusMsg, agentDaemonStateMsg, agentLockResultMsg, agentUnlockResultMsg, vaultStatusMsg, vaultPollMsg, foundKeysMsg, ButtonFlashDoneMsg:
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
		return s, addKeyToAgentCmd(s.socketPath, msg.Path)

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
		if s.sk != nil {
			s.sk.UpdateVaultState("", false, false)
		}
		return s, nil

	case ButtonFlashDoneMsg:
		s.buttons.ClearPress()
		return s, nil

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
		if s.focus == agentFocusFound {
			visible := s.visibleFoundKeys()
			if len(visible) == 0 {
				s.focusFirstLoadedKey()
				s.foundSelected = 0
			} else if s.foundSelected > s.foundKeysMaxIndex(visible) {
				s.foundSelected = s.foundKeysMaxIndex(visible)
			}
		}
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
		if s.sk != nil {
			s.sk.UpdateVaultState(s.vaultMode, s.vaultLocked, s.vaultKnown)
		}
		return s, nil

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
		return s.handleMouse(msg.X, msg.Y)

	case tea.MouseReleaseMsg:
		if msg.Button != tea.MouseLeft || s.fileSelector.Visible() || s.showPass || s.commentOverlay.active {
			return s, nil
		}
		return s.handleMouse(msg.X, msg.Y)

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
			return s, s.commentOverlay.Update(msg, s.socketPath)
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

func (s *AgentScreen) handleMouse(x, y int) (tea.Model, tea.Cmd) {
	rows := s.keyTable.Table.Rows()
	for i := range rows {
		if inZoneBounds(fmt.Sprintf("%skey-%d", s.zonePrefix, i), x, y) {
			s.focus = agentFocusTable
			s.keyTable.Table.SetCursor(i)
			s.syncTableSelection()
			return s, nil
		}
	}
	if row := s.keyTable.HandleMouse(x, y); row >= 0 {
		s.focus = agentFocusTable
		s.keyTable.Table.SetCursor(row)
		s.syncTableSelection()
		return s, nil
	}
	visible := s.visibleFoundKeys()
	for i := range visible {
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

func (s *AgentScreen) removeSelectedKey() (tea.Model, tea.Cmd) {
	row := s.keyTable.SelectedRow()
	if row == nil {
		return s, nil
	}
	fp := row[1]
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
	path := visible[s.foundSelected]
	return s, addKeyToAgentCmd(s.socketPath, path)
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

func (s *AgentScreen) foundKeysMaxIndex(visible []string) int {
	if len(visible) == 0 {
		return 0
	}
	maxIdx := len(visible) - 1
	if maxIdx >= foundKeysMaxVisible {
		maxIdx = foundKeysMaxVisible - 1
	}
	return maxIdx
}

func sectionBoxWidth(width int) int {
	boxW := width * 3 / 4
	if boxW > sectionBoxMaxWidth {
		boxW = sectionBoxMaxWidth
	}
	if boxW < sectionBoxMinWidth {
		boxW = sectionBoxMinWidth
	}
	return boxW
}

func (s *AgentScreen) visibleFoundKeys() []string {
	var visible []string
	for _, p := range s.foundKeys {
		pubKey, _, _, err := agent.ParseKeyFromPath(p)
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

	var sections []string
	sections = append(sections, s.renderLoadedKeys(w, active))

	visible := s.visibleFoundKeys()
	if len(visible) > 0 {
		sections = append(sections, "")
		foundContent := s.renderFoundKeys(visible, w, active)
		sections = append(sections, foundContent)
	}

	content := strings.Join(sections, "\n")
	contentLines := strings.Count(content, "\n") + 1
	if padTop := (height - contentLines) / 2; padTop > 0 {
		content = strings.Repeat("\n", padTop) + content
	}
	return tea.NewView(content)
}

func (s *AgentScreen) BannerColor() color.Color {
	t := s.sk.Theme()
	if s.statusErr {
		c, _ := theme.HexToRGBA(t.Error)
		return c
	}
	if s.daemonRunning {
		c, _ := theme.HexToRGBA(t.Focus)
		return c
	}
	c, _ := theme.HexToRGBA(t.Accent)
	return c
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

func (s *AgentScreen) ControlButtonsView(focused bool) string {
	st := s.sk.Styles()
	var parts []string
	for i, label := range s.buttons.Labels {
		var style lipgloss.Style
		switch {
		case s.buttons.Pressed == i:
			style = st.HeaderTabActiveFocused
		case s.buttons.Active == i && focused:
			style = st.HeaderTabActiveFocused
		case s.buttons.Active == i:
			style = st.HeaderTabActiveUnfocused
		default:
			style = st.HeaderTabInactive
		}
		rendered := style.Render(label)
		if s.buttons.ZonePrefix != "" {
			rendered = zone.Mark(s.buttons.ZonePrefix+label, rendered)
		}
		parts = append(parts, rendered)
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, parts...)
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

	content := border.Render(s.keyTable.AgentViewMarked(s.zonePrefix, focused, st))
	return lipgloss.Place(width, 0, lipgloss.Center, lipgloss.Top, title+"\n"+content)
}

func (s *AgentScreen) renderFoundKeys(visible []string, width int, active bool) string {
	st := s.sk.Styles()
	title := st.SectionTitleStyle.Render(" Found Keys")
	var lines []string
	maxShow := foundKeysMaxVisible
	if len(visible) < maxShow {
		maxShow = len(visible)
	}
	for i := 0; i < maxShow; i++ {
		style := st.AccentStyle
		linePrefix := "  "
		if active && s.focus == agentFocusFound && i == s.foundSelected {
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color(s.sk.Theme().Focus)).Bold(true)
			linePrefix = "> "
		}
		rendered := style.Render(linePrefix + utils.DisplayPath(visible[i]))
		rendered = zone.Mark(fmt.Sprintf("%sfound-%d", s.zonePrefix, i), rendered)
		lines = append(lines, rendered)
	}
	if len(visible) > maxShow {
		lines = append(lines, st.DimStyle.Render(fmt.Sprintf("  ... and %d more", len(visible)-maxShow)))
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

func (s *AgentScreen) HelpEntries() []string {
	st := s.sk.Styles()
	return []string{
		st.HelpRow("Agent controls", ""),
		st.HelpRow("a", "Add key"),
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
		keys, err := agent.ListKeysFromSocket(socketPath)
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
		removed, err := agent.RemoveKeyFromSocketByFingerprint(socketPath, fingerprint)
		if err != nil {
			return agentStatusMsg{text: "agent not running", isErr: true}
		}
		if !removed {
			return agentStatusMsg{text: "key not found", isErr: true}
		}
		return agentStatusMsg{text: "key removed"}
	}
}

func addKeyToAgentCmd(socketPath, path string) tea.Cmd {
	return func() tea.Msg {
		// Default autoload on for vault (same as CLI without --no-autoload).
		if err := vault.AddPrivateKeyFileToSocket(socketPath, path, true); err != nil {
			return agentStatusMsg{text: "add failed: " + err.Error(), isErr: true}
		}
		return agentStatusMsg{text: "key added: " + utils.DisplayPath(path)}
	}
}

func lockAgentCmd(socketPath, passphrase string) tea.Cmd {
	return func() tea.Msg {
		return agentLockResultMsg{err: agent.LockSocket(socketPath, []byte(passphrase))}
	}
}

// lockVaultCmd locks the vault agent immediately. Locking needs no passphrase: the
// daemon wipes the in-memory master key.
func lockVaultCmd(socketPath string) tea.Cmd {
	return func() tea.Msg {
		return agentLockResultMsg{err: agent.LockSocket(socketPath, nil)}
	}
}

func unlockAgentCmd(socketPath, passphrase string) tea.Cmd {
	return func() tea.Msg {
		return agentUnlockResultMsg{err: agent.UnlockSocket(socketPath, []byte(passphrase))}
	}
}

// checkVaultState queries the running agent for its backend mode and, for vault agents,
// whether the vault is locked.
func checkVaultState(socketPath string) vaultStatusMsg {
	mode, live := agent.LiveBackendMode(socketPath)
	if !live {
		return vaultStatusMsg{}
	}
	if mode != "vault" {
		return vaultStatusMsg{mode: mode, reachable: true}
	}
	resp, err := agent.CallExtension(socketPath, vault.ExtensionVaultLocked, nil)
	if err != nil || len(resp) != 1 {
		return vaultStatusMsg{mode: mode}
	}
	return vaultStatusMsg{mode: mode, reachable: true, locked: resp[0] == 1}
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

func discoverKeysCmd() tea.Cmd {
	return func() tea.Msg {
		return foundKeysMsg{paths: utils.DiscoverKeyPaths([]string{}, true, true, false)}
	}
}
