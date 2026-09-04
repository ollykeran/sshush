package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vaultops"
)

// vaultIdentityRow is one row of the vault identity table, already formatted for display.
type vaultIdentityRow struct {
	fingerprint string
	loaded      string // "yes"/"no"/"n/a"
	autoload    bool
	comment     string
	keyType     string
}

// vaultIdentitiesMsg reports the result of listing vault identities. When err is nil
// and initialized is false, the vault file at vaultPath does not exist yet (not an
// error state: this is the normal pre-init condition).
type vaultIdentitiesMsg struct {
	rows        []vaultIdentityRow
	initialized bool
	err         error
}

// vaultOpResultMsg reports the result of an add/remove/load/autoload/unlock/lock op.
type vaultOpResultMsg struct {
	status string
	err    error
}

// vaultInitResultMsg reports the result of initializing a vault. mnemonic is non-empty
// only when recovery was generated (the caller must show it once and never store it
// anywhere but recoveryFile).
type vaultInitResultMsg struct {
	mnemonic     string
	recoveryFile string
	err          error
}

// Button action ids, stable across renders even though which ids are visible
// (and at which row) varies with state — see visibleButtons/syncButtons.
const (
	vBtnInit = iota
	vBtnAdd
	vBtnRemove
	vBtnLoad
	vBtnAutoloadOn
	vBtnAutoloadOff
	vBtnUnlock
	vBtnRecovery
	vBtnLock
)

const (
	vaultFocusTable = iota
	vaultFocusButtons
)

// VaultScreen is the vault tab: identity table, init/add/remove/load/autoload/unlock/lock actions.
// Registered only when the configured agent backend mode is vault (see NewTUI).
type VaultScreen struct {
	sk         *Skeleton
	configPath string
	socketPath string
	vaultPath  string

	table      table.Model
	buttons    ButtonRow
	zonePrefix string

	rows             []vaultIdentityRow
	vaultInitialized bool

	fileSelector *FileSelector

	passInput      textinput.Model
	showPass       bool
	passAction     string // "init-pass", "init-confirm", "unlock"
	initPassphrase []byte

	recoveryInput textinput.Model
	showRecovery  bool

	recoveryDisplay struct {
		visible bool
		phrase  string
		file    string
	}

	status    string
	statusErr bool
	width     int
	height    int
	focus     int
}

// NewVaultScreen creates a VaultScreen for the given skeleton, config path, socket path,
// and resolved (or unresolved) vault path from config.
func NewVaultScreen(sk *Skeleton, configPath, socketPath, vaultPath string) *VaultScreen {
	prefix := zone.NewPrefix()

	pi := textinput.New()
	pi.Placeholder = "passphrase"
	pi.EchoMode = textinput.EchoPassword
	pi.EchoCharacter = '*'

	ri := textinput.New()
	ri.Placeholder = "24-word recovery phrase"

	btns := NewButtonRow()
	btns.ZonePrefix = prefix + "ctrl-"

	innerW := keyBoxInnerWidth(defaultViewWidth)
	rowW := innerW + keyCellPadOverhead
	t := table.New(
		table.WithColumns(vaultTableColumns(innerW)),
		table.WithRows([]table.Row{}),
		table.WithFocused(true),
		table.WithHeight(defaultKeyTableHeight),
		table.WithWidth(rowW),
	)
	t.SetStyles(vaultTableStyles(rowW, sk.Styles(), true))

	return &VaultScreen{
		sk:            sk,
		configPath:    configPath,
		socketPath:    socketPath,
		vaultPath:     vaultPath,
		table:         t,
		buttons:       btns,
		zonePrefix:    prefix,
		fileSelector:  NewFileSelector(ModeLoadFile, "Select key file", sk.Styles()),
		passInput:     pi,
		recoveryInput: ri,
		status:        "loading...",
		focus:         vaultFocusTable,
	}
}

func (s *VaultScreen) HasModal() bool {
	return s.fileSelector.Visible() || s.showPass || s.showRecovery || s.recoveryDisplay.visible
}

func (s *VaultScreen) HasActiveTextInput() bool {
	return s.showPass || s.showRecovery
}

func (s *VaultScreen) Init() tea.Cmd {
	return listVaultIdentitiesCmd(s.vaultPath, s.socketPath)
}

// Refresh re-lists vault identities. Called by Skeleton whenever the Vault tab becomes
// active, so state changed elsewhere (e.g. an unlock done from the Agent tab, or a vault
// initialized/populated after this screen's Init already ran) is picked up on tab switch.
func (s *VaultScreen) Refresh() tea.Cmd {
	return listVaultIdentitiesCmd(s.vaultPath, s.socketPath)
}

// HandleDKey lets Skeleton's global 'd' key remove the selected identity when the
// identity table is focused, instead of entering daemon focus (see skeleton.go).
func (s *VaultScreen) HandleDKey() (tea.Cmd, bool) {
	if s.focus != vaultFocusTable || s.HasModal() {
		return nil, false
	}
	_, cmd := s.removeSelected()
	return cmd, true
}

func (s *VaultScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if s.fileSelector.Visible() {
		switch msg.(type) {
		case tea.WindowSizeMsg, FileSelectedMsg, FilePickerCancelledMsg, vaultIdentitiesMsg, vaultOpResultMsg, vaultInitResultMsg, ButtonFlashDoneMsg:
			// Handle these below.
		default:
			return s, s.fileSelector.Update(msg)
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.resizeTable()
		s.fileSelector.SetHeight(max(s.height-fileSelectorHeightReserve, fileSelectorMinHeight))
		return s, nil

	case ThemeChangedMsg:
		s.resizeTable()
		return s, nil

	case FileSelectedMsg:
		s.fileSelector.Hide()
		s.focus = vaultFocusTable
		return s, addVaultKeyCmd(s.socketPath, msg.Path, true)

	case FilePickerCancelledMsg:
		s.fileSelector.Hide()
		s.focus = vaultFocusTable
		return s, nil

	case ButtonFlashDoneMsg:
		s.buttons.ClearPress()
		return s, nil

	case vaultIdentitiesMsg:
		if msg.err != nil {
			s.status = msg.err.Error()
			s.statusErr = true
			s.rows = nil
			s.table.SetRows(nil)
			s.resizeTable()
			return s, nil
		}
		s.vaultInitialized = msg.initialized
		if !msg.initialized {
			s.rows = nil
			s.table.SetRows(nil)
			s.resizeTable()
			s.statusErr = false
			s.status = "vault not initialized - press i to initialize"
			return s, nil
		}
		s.rows = msg.rows
		rows := make([]table.Row, len(msg.rows))
		for i, r := range msg.rows {
			autoload := "off"
			if r.autoload {
				autoload = "on"
			}
			rows[i] = table.Row{r.fingerprint, r.loaded, autoload, r.comment, r.keyType}
		}
		s.table.SetRows(rows)
		if len(rows) > 0 && s.table.Cursor() < 0 {
			s.table.SetCursor(0)
		}
		s.resizeTable()
		s.statusErr = false
		if len(rows) == 0 {
			s.status = "no identities in vault"
		} else {
			s.status = fmt.Sprintf("%d identity(ies) in vault", len(rows))
		}
		return s, nil

	case vaultOpResultMsg:
		if msg.err != nil {
			s.status = msg.err.Error()
			s.statusErr = true
			return s, nil
		}
		s.status = msg.status
		s.statusErr = false
		return s, listVaultIdentitiesCmd(s.vaultPath, s.socketPath)

	case vaultInitResultMsg:
		if msg.err != nil {
			s.status = msg.err.Error()
			s.statusErr = true
			return s, nil
		}
		s.statusErr = false
		s.vaultInitialized = true
		if msg.mnemonic != "" {
			s.recoveryDisplay.visible = true
			s.recoveryDisplay.phrase = msg.mnemonic
			s.recoveryDisplay.file = msg.recoveryFile
		}
		s.status = "vault initialized"
		return s, listVaultIdentitiesCmd(s.vaultPath, s.socketPath)

	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft || s.fileSelector.Visible() || s.showPass || s.showRecovery || s.recoveryDisplay.visible {
			return s, nil
		}
		return s.handleMouse(msg.X, msg.Y)

	case tea.MouseReleaseMsg:
		if msg.Button != tea.MouseLeft || s.fileSelector.Visible() || s.showPass || s.showRecovery || s.recoveryDisplay.visible {
			return s, nil
		}
		return s.handleMouse(msg.X, msg.Y)

	case tea.KeyPressMsg:
		if s.recoveryDisplay.visible {
			s.recoveryDisplay.visible = false
			return s, nil
		}
		if s.showPass {
			return s.handlePassInput(msg)
		}
		if s.showRecovery {
			return s.handleRecoveryInput(msg)
		}
		if s.fileSelector.Visible() {
			return s, s.fileSelector.Update(msg)
		}
		return s.handleKeys(msg)
	}

	if s.focus == vaultFocusTable {
		var cmd tea.Cmd
		s.table, cmd = s.table.Update(msg)
		return s, cmd
	}
	return s, nil
}

func (s *VaultScreen) handleKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		return s, tea.Quit

	case "up", "k":
		switch s.focus {
		case vaultFocusTable:
			if s.table.Cursor() > 0 {
				var cmd tea.Cmd
				s.table, cmd = s.table.Update(msg)
				return s, cmd
			}
			return s, navToTabBarCmd()
		case vaultFocusButtons:
			s.focus = vaultFocusTable
			s.buttons.Focused = false
			s.syncTableSelection()
		}
		return s, nil

	case "down", "j":
		if s.focus == vaultFocusTable {
			rows := s.table.Rows()
			cursor := s.table.Cursor()
			if len(rows) > 0 && cursor < len(rows)-1 {
				var cmd tea.Cmd
				s.table, cmd = s.table.Update(msg)
				return s, cmd
			}
			s.focus = vaultFocusButtons
			s.buttons.Focused = true
			s.syncTableSelection()
		}
		return s, nil

	case "left":
		if s.focus == vaultFocusButtons {
			s.buttons.Left()
		}
		return s, nil

	case "right":
		if s.focus == vaultFocusButtons {
			s.buttons.Right()
		}
		return s, nil

	case "enter":
		if s.focus == vaultFocusButtons {
			ids := s.syncButtons()
			if s.buttons.Active < len(ids) {
				return s.pressButton(s.buttons.Active, ids[s.buttons.Active])
			}
		}
		return s, nil

	case "i":
		return s, s.startInit()

	case "a":
		s.focus = vaultFocusTable
		return s, s.fileSelector.Show()

	case "backspace", "delete":
		return s.removeSelected()

	case "o":
		return s.loadSelected()

	case "+":
		return s.setAutoloadSelected(true)

	case "-":
		return s.setAutoloadSelected(false)

	case "U":
		return s, s.startUnlockPassphrase()

	case "R":
		return s, s.startUnlockRecovery()

	case "l":
		return s, s.startLock()
	}

	if s.focus == vaultFocusTable {
		var cmd tea.Cmd
		s.table, cmd = s.table.Update(msg)
		return s, cmd
	}
	return s, nil
}

func (s *VaultScreen) handlePassInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.showPass = false
		s.passInput.Blur()
		s.initPassphrase = nil
		return s, nil
	case "enter":
		value := []byte(s.passInput.Value())
		switch s.passAction {
		case "init-pass":
			if len(value) == 0 {
				s.status = "passphrase cannot be empty"
				s.statusErr = true
				return s, nil
			}
			s.initPassphrase = value
			s.passAction = "init-confirm"
			s.passInput.SetValue("")
			s.passInput.Placeholder = "confirm passphrase"
			return s, nil
		case "init-confirm":
			if string(value) != string(s.initPassphrase) {
				s.status = "passphrases do not match"
				s.statusErr = true
				s.passAction = "init-pass"
				s.passInput.SetValue("")
				s.passInput.Placeholder = "new vault passphrase"
				s.initPassphrase = nil
				return s, nil
			}
			s.showPass = false
			s.passInput.Blur()
			passphrase := s.initPassphrase
			s.initPassphrase = nil
			s.status = "initializing..."
			s.statusErr = false
			return s, initVaultCmd(s.vaultPath, passphrase, true)
		case "unlock":
			s.showPass = false
			s.passInput.Blur()
			s.status = "unlocking..."
			s.statusErr = false
			return s, unlockVaultPassphraseCmd(s.socketPath, value)
		}
		return s, nil
	}
	var cmd tea.Cmd
	s.passInput, cmd = s.passInput.Update(msg)
	return s, cmd
}

func (s *VaultScreen) handleRecoveryInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.showRecovery = false
		s.recoveryInput.Blur()
		return s, nil
	case "enter":
		mnemonic := strings.TrimSpace(s.recoveryInput.Value())
		s.showRecovery = false
		s.recoveryInput.Blur()
		s.status = "unlocking..."
		s.statusErr = false
		return s, unlockVaultRecoveryCmd(s.socketPath, mnemonic)
	}
	var cmd tea.Cmd
	s.recoveryInput, cmd = s.recoveryInput.Update(msg)
	return s, cmd
}

func (s *VaultScreen) handleMouse(x, y int) (tea.Model, tea.Cmd) {
	rows := s.table.Rows()
	for i := range rows {
		if inZoneBounds(fmt.Sprintf("%skey-%d", s.zonePrefix, i), x, y) {
			s.focus = vaultFocusTable
			s.buttons.Focused = false
			s.table.SetCursor(i)
			s.syncTableSelection()
			return s, nil
		}
	}
	ids := s.syncButtons()
	if btn := s.buttons.HandleMouse(x, y); btn >= 0 {
		return s.pressButton(btn, ids[btn])
	}
	return s, nil
}

// vaultButtonDesc is one entry in the button row: its stable action id, current
// label, and whether it should render disabled given current vault/lock state.
type vaultButtonDesc struct {
	id       int
	label    string
	disabled bool
}

// visibleButtons returns the button row contents for the current state: Init only
// appears before the vault is initialized; Unlock/Recovery grey out once the vault
// is known to be unlocked, and Lock greys out once it's known to be locked.
func (s *VaultScreen) visibleButtons() []vaultButtonDesc {
	var out []vaultButtonDesc
	if !s.vaultInitialized {
		out = append(out, vaultButtonDesc{vBtnInit, "[i]nit", false})
	}
	unlockDisabled := s.sk.vaultKnown && !s.sk.vaultLocked
	lockDisabled := s.sk.vaultKnown && s.sk.vaultLocked
	out = append(out,
		vaultButtonDesc{vBtnAdd, "[a]dd", false},
		vaultButtonDesc{vBtnRemove, "[d]elete", false},
		vaultButtonDesc{vBtnLoad, "[o]load", false},
		vaultButtonDesc{vBtnAutoloadOn, "[+]auto", false},
		vaultButtonDesc{vBtnAutoloadOff, "[-]auto", false},
		vaultButtonDesc{vBtnUnlock, "[U]nlock", unlockDisabled},
		vaultButtonDesc{vBtnRecovery, "[R]ecovery", unlockDisabled},
		vaultButtonDesc{vBtnLock, "[l]ock", lockDisabled},
	)
	return out
}

// syncButtons rebuilds s.buttons.Labels/Disabled from current state and returns the
// parallel slice mapping each visible button's row position to its stable action id.
func (s *VaultScreen) syncButtons() []int {
	descs := s.visibleButtons()
	labels := make([]string, len(descs))
	ids := make([]int, len(descs))
	disabled := make(map[int]bool, len(descs))
	for i, d := range descs {
		labels[i] = d.label
		ids[i] = d.id
		if d.disabled {
			disabled[i] = true
		}
	}
	s.buttons.Labels = labels
	s.buttons.Disabled = disabled
	if s.buttons.Active >= len(labels) {
		s.buttons.Active = len(labels) - 1
	}
	if s.buttons.Active < 0 {
		s.buttons.Active = 0
	}
	return ids
}

func (s *VaultScreen) pressButton(row, id int) (tea.Model, tea.Cmd) {
	s.buttons.Active = row
	if s.buttons.Disabled[row] {
		return s, nil
	}
	s.buttons.Press()
	var action tea.Cmd
	switch id {
	case vBtnInit:
		action = s.startInit()
	case vBtnAdd:
		action = s.fileSelector.Show()
	case vBtnRemove:
		_, action = s.removeSelected()
	case vBtnLoad:
		_, action = s.loadSelected()
	case vBtnAutoloadOn:
		_, action = s.setAutoloadSelected(true)
	case vBtnAutoloadOff:
		_, action = s.setAutoloadSelected(false)
	case vBtnUnlock:
		action = s.startUnlockPassphrase()
	case vBtnRecovery:
		action = s.startUnlockRecovery()
	case vBtnLock:
		action = s.startLock()
	}
	return s, tea.Batch(action, ButtonFlashCmd())
}

func (s *VaultScreen) startInit() tea.Cmd {
	s.showPass = true
	s.passAction = "init-pass"
	s.initPassphrase = nil
	s.passInput.SetValue("")
	s.passInput.Placeholder = "new vault passphrase"
	return s.passInput.Focus()
}

// startUnlockPassphrase opens the unlock-passphrase prompt, unless the shared vault
// lock state already says the vault is unlocked (mirrors the Unlock button's grey-out).
func (s *VaultScreen) startUnlockPassphrase() tea.Cmd {
	if s.sk.vaultKnown && !s.sk.vaultLocked {
		s.status = "vault already unlocked"
		s.statusErr = false
		return nil
	}
	s.showPass = true
	s.passAction = "unlock"
	s.passInput.SetValue("")
	s.passInput.Placeholder = "unlock passphrase"
	return s.passInput.Focus()
}

// startUnlockRecovery opens the recovery-phrase prompt, unless the vault is already
// known to be unlocked.
func (s *VaultScreen) startUnlockRecovery() tea.Cmd {
	if s.sk.vaultKnown && !s.sk.vaultLocked {
		s.status = "vault already unlocked"
		s.statusErr = false
		return nil
	}
	s.showRecovery = true
	s.recoveryInput.SetValue("")
	return s.recoveryInput.Focus()
}

// startLock locks the vault agent, unless it's already known to be locked.
func (s *VaultScreen) startLock() tea.Cmd {
	if s.sk.vaultKnown && s.sk.vaultLocked {
		s.status = "vault already locked"
		s.statusErr = false
		return nil
	}
	s.status = "locking..."
	s.statusErr = false
	return vaultLockCmd(s.socketPath)
}

func (s *VaultScreen) selectedFingerprint() string {
	row := s.table.SelectedRow()
	if len(row) == 0 {
		return ""
	}
	return row[0]
}

func (s *VaultScreen) removeSelected() (tea.Model, tea.Cmd) {
	fp := s.selectedFingerprint()
	if fp == "" {
		return s, nil
	}
	return s, removeVaultIdentityCmd(s.socketPath, s.vaultPath, fp)
}

func (s *VaultScreen) loadSelected() (tea.Model, tea.Cmd) {
	fp := s.selectedFingerprint()
	if fp == "" {
		return s, nil
	}
	return s, sessionLoadVaultCmd(s.socketPath, fp)
}

func (s *VaultScreen) setAutoloadSelected(on bool) (tea.Model, tea.Cmd) {
	fp := s.selectedFingerprint()
	if fp == "" {
		return s, nil
	}
	return s, setVaultAutoloadCmd(s.socketPath, fp, on)
}

func (s *VaultScreen) resizeTable() {
	w := s.width
	if w < 1 {
		w = defaultViewWidth
	}
	innerW := keyBoxInnerWidth(w)
	rowW := innerW + keyCellPadOverhead
	s.table.SetColumns(vaultTableColumns(innerW))
	s.table.SetWidth(rowW)
	s.table.SetHeight(s.tableHeight())
	s.syncTableSelection()
}

// syncTableSelection keeps row data visible and toggles the cursor-row highlight only
// while the identity table is the active focus target (mirrors AgentScreen).
func (s *VaultScreen) syncTableSelection() {
	rows := s.table.Rows()
	if len(rows) > 0 && s.table.Cursor() < 0 {
		s.table.SetCursor(0)
	}
	highlighted := s.sk.ScreenActive() && s.focus == vaultFocusTable && len(rows) > 0
	s.table.SetStyles(vaultTableStyles(s.table.Width(), s.sk.Styles(), highlighted))
}

func (s *VaultScreen) tableHeight() int {
	rowCount := len(s.table.Rows())
	if rowCount == 0 {
		return minTableHeight
	}
	h := rowCount + 2
	if h > agentLoadedKeysMaxRows {
		h = agentLoadedKeysMaxRows
	}
	if h < minTableHeight {
		h = minTableHeight
	}
	return h
}

// renderTable renders the identity table with the same per-row cell-background
// highlight AgentScreen's KeyTable uses (via renderVaultTableRow/vaultTableStyles),
// rather than bubbles/table's own cursor rendering, so focus styling matches exactly.
func (s *VaultScreen) renderTable(st Styles, highlightCursor bool) string {
	styles := vaultTableStyles(s.table.Width(), st, highlightCursor)
	cols := s.table.Columns()
	rows := s.table.Rows()
	cursor := s.table.Cursor()

	out := s.tableHeaderLines()
	for r, row := range rows {
		selected := highlightCursor && r == cursor
		line := renderVaultTableRow(row, cols, styles, selected)
		line = zone.Mark(fmt.Sprintf("%skey-%d", s.zonePrefix, r), line)
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// tableHeaderLines reuses bubbles/table's own header rendering (header text + rule)
// so only the data rows below are rendered manually.
func (s *VaultScreen) tableHeaderLines() []string {
	lines := strings.Split(s.table.View(), "\n")
	if len(lines) >= 2 {
		return lines[:2]
	}
	return lines
}

func renderVaultTableRow(row table.Row, cols []table.Column, styles table.Styles, selected bool) string {
	var parts []string
	for i, value := range row {
		if i >= len(cols) || cols[i].Width <= 0 {
			continue
		}
		w := cols[i].Width
		box := lipgloss.NewStyle().Width(w).MaxWidth(w).Inline(true)
		text := box.Render(ansi.Truncate(value, w, "…"))
		if selected {
			parts = append(parts, styles.Selected.Render(text))
		} else {
			parts = append(parts, styles.Cell.Render(text))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func (s *VaultScreen) View() tea.View {
	width := s.width
	height := s.height
	if width < 1 {
		width = defaultViewWidth
	}
	if height < 1 {
		height = defaultViewHeight
	}
	active := s.sk.ScreenActive()
	st := s.sk.Styles()

	if s.fileSelector.Visible() {
		innerW := width - 2
		if innerW < 1 {
			innerW = 1
		}
		return tea.NewView(lipgloss.Place(innerW, height, lipgloss.Center, lipgloss.Center,
			s.fileSelector.View(width, height, active, st)))
	}

	if s.showPass {
		title := "Enter passphrase"
		switch s.passAction {
		case "init-pass":
			title = "Set new vault passphrase"
		case "init-confirm":
			title = "Confirm vault passphrase"
		case "unlock":
			title = "Enter unlock passphrase"
		}
		rendered := st.SectionTitleStyle.Render(title)
		return tea.NewView(lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			rendered+"\n"+st.FocusedBorderStyle.Render(s.passInput.View())))
	}

	if s.showRecovery {
		title := st.SectionTitleStyle.Render("Enter 24-word recovery phrase")
		return tea.NewView(lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			title+"\n"+st.FocusedBorderStyle.Render(s.recoveryInput.View())))
	}

	if s.recoveryDisplay.visible {
		title := st.SectionTitleStyle.Render("Vault initialized — recovery phrase (write these down)")
		boxW := sectionBoxWidth(width) - 4
		body := lipgloss.NewStyle().Width(boxW).Render(s.recoveryDisplay.phrase)
		box := st.FocusedBorderStyle.Width(boxW).Render(body)
		hint := st.DimStyle.Render("Also written to " + utils.DisplayPath(s.recoveryDisplay.file) + " (mode 0600). Press any key to continue.")
		return tea.NewView(lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			title+"\n"+box+"\n"+hint))
	}

	title := st.SectionTitleStyle.Render(" Vault Identities")
	border := st.UnfocusedBorderStyle
	if s.sk.vaultKnown && s.sk.vaultLocked {
		border = st.WarnBorderStyle
	} else if active && s.focus == vaultFocusTable {
		border = st.FocusedBorderStyle
	}
	tableHighlighted := active && s.focus == vaultFocusTable && len(s.table.Rows()) > 0
	tableBox := border.Render(s.renderTable(st, tableHighlighted))

	s.syncButtons()
	buttonsLine := s.buttons.View(st)
	pathLine := st.DimStyle.Render("vault: " + utils.DisplayPath(s.vaultPath))

	content := lipgloss.Place(width, 0, lipgloss.Center, lipgloss.Top,
		title+"\n"+tableBox+"\n\n"+buttonsLine+"\n"+pathLine)
	contentLines := strings.Count(content, "\n") + 1
	if padTop := (height - contentLines) / 2; padTop > 0 {
		content = strings.Repeat("\n", padTop) + content
	}
	return tea.NewView(content)
}

func (s *VaultScreen) StatusTextRaw() (string, bool) {
	return s.status, s.statusErr
}

func (s *VaultScreen) HelpEntries() []string {
	st := s.sk.Styles()
	return []string{
		st.HelpRow("Vault controls", ""),
		st.HelpRow("i", "Init vault (until initialized)"),
		st.HelpRow("a", "Add key"),
		st.HelpRow("d / bksp", "Remove identity"),
		st.HelpRow("o", "Session-load identity"),
		st.HelpRow("+ / -", "Autoload on / off"),
		st.HelpRow("U", "Unlock (passphrase)"),
		st.HelpRow("R", "Unlock (recovery phrase)"),
		st.HelpRow("l", "Lock vault"),
		st.HelpRow("down at last row", "Move to buttons; up returns to table"),
		"",
	}
}

// vaultTableColumns lays out the 5 vault identity columns within width w.
func vaultTableColumns(w int) []table.Column {
	if w < keyTableMinTotalWidth {
		w = keyTableMinTotalWidth
	}
	fpW := 44
	loadedW := 8
	autoloadW := 10
	typeW := 12
	commentW := w - fpW - loadedW - autoloadW - typeW
	if commentW < 12 {
		commentW = 12
	}
	return []table.Column{
		{Title: "Fingerprint", Width: fpW},
		{Title: "Loaded", Width: loadedW},
		{Title: "Autoload", Width: autoloadW},
		{Title: "Comment", Width: commentW},
		{Title: "Type", Width: typeW},
	}
}

// vaultTableStyles reuses keyTableStyles so the vault table matches the agent key
// table's colors and selection styling exactly.
func vaultTableStyles(rowWidth int, st Styles, highlightCursor bool) table.Styles {
	styles := keyTableStyles(rowWidth, st)
	if !highlightCursor {
		styles.Selected = lipgloss.NewStyle().
			Foreground(lipgloss.Color(st.TableCellFgHex)).
			Padding(0, 1)
	}
	return styles
}

// Commands
//
// Every command here is a tea.Cmd wrapper over internal/vaultops: the vault
// work itself is shared with the CLI, and what is left is turning a typed
// result into the screen's own message vocabulary. The Env these build never
// sets AskPassphrase, because a tea.Cmd cannot block for input — the screen's
// passphrase modal does that, and drives unlockVaultPassphraseCmd itself.

// vaultEnv is the environment a vault command runs in. vaultPath is empty for
// the commands that only need the agent, which then resolve a selected row by
// its fingerprint alone.
func vaultEnv(vaultPath, socketPath string) vaultops.Env {
	return vaultops.Env{VaultPath: vaultPath, SocketPath: socketPath}
}

// vaultStatusErr flattens a vaultops failure into the one sentence the status
// line shows, keeping the remedy the CLI prints on a second line rather than
// dropping it.
func vaultStatusErr(err error) error {
	if hint := vaultops.HintOf(err); hint != "" {
		return fmt.Errorf("%s — %s", err.Error(), hint)
	}
	return err
}

func listVaultIdentitiesCmd(vaultPath, socketPath string) tea.Cmd {
	return func() tea.Msg {
		res, err := vaultops.List(vaultEnv(vaultPath, socketPath))
		if err != nil {
			return vaultIdentitiesMsg{err: vaultStatusErr(err)}
		}
		if !res.Initialized {
			return vaultIdentitiesMsg{initialized: false}
		}
		rows := make([]vaultIdentityRow, len(res.Identities))
		for i, id := range res.Identities {
			rows[i] = vaultIdentityRow{
				fingerprint: id.Fingerprint,
				loaded:      id.Loaded.String(),
				autoload:    id.Autoload,
				comment:     id.Comment,
				keyType:     id.KeyType,
			}
		}
		return vaultIdentitiesMsg{rows: rows, initialized: true}
	}
}

func initVaultCmd(vaultPath string, passphrase []byte, withRecovery bool) tea.Cmd {
	return func() tea.Msg {
		defer func() {
			for i := range passphrase {
				passphrase[i] = 0
			}
		}()
		res, err := vaultops.Init(vaultEnv(vaultPath, ""), passphrase, vaultops.InitOptions{Recovery: withRecovery})
		if err != nil {
			return vaultInitResultMsg{err: vaultStatusErr(err)}
		}
		return vaultInitResultMsg{mnemonic: res.Mnemonic, recoveryFile: res.RecoveryFile}
	}
}

func addVaultKeyCmd(socketPath, path string, autoload bool) tea.Cmd {
	return func() tea.Msg {
		if _, err := vaultops.Add(vaultEnv("", socketPath), []string{path}, autoload); err != nil {
			return vaultOpResultMsg{err: vaultStatusErr(err)}
		}
		return vaultOpResultMsg{status: "key added: " + utils.DisplayPath(path)}
	}
}

func removeVaultIdentityCmd(socketPath, vaultPath, fingerprint string) tea.Cmd {
	return func() tea.Msg {
		if _, err := vaultops.Remove(vaultEnv(vaultPath, socketPath), []string{fingerprint}); err != nil {
			return vaultOpResultMsg{err: vaultStatusErr(err)}
		}
		return vaultOpResultMsg{status: "identity removed"}
	}
}

func sessionLoadVaultCmd(socketPath, fingerprint string) tea.Cmd {
	return func() tea.Msg {
		if _, err := vaultops.SessionLoad(vaultEnv("", socketPath), []string{fingerprint}); err != nil {
			return vaultOpResultMsg{err: vaultStatusErr(err)}
		}
		return vaultOpResultMsg{status: "loaded into agent session"}
	}
}

func setVaultAutoloadCmd(socketPath, fingerprint string, on bool) tea.Cmd {
	return func() tea.Msg {
		if _, err := vaultops.SetAutoload(vaultEnv("", socketPath), []string{fingerprint}, on); err != nil {
			return vaultOpResultMsg{err: vaultStatusErr(err)}
		}
		state := "off"
		if on {
			state = "on"
		}
		return vaultOpResultMsg{status: "autoload " + state}
	}
}

func unlockVaultPassphraseCmd(socketPath string, passphrase []byte) tea.Cmd {
	return func() tea.Msg {
		defer func() {
			for i := range passphrase {
				passphrase[i] = 0
			}
		}()
		if err := vaultops.UnlockPassphrase(vaultEnv("", socketPath), passphrase); err != nil {
			return vaultOpResultMsg{err: vaultStatusErr(err)}
		}
		return vaultOpResultMsg{status: "vault unlocked"}
	}
}

func unlockVaultRecoveryCmd(socketPath, mnemonic string) tea.Cmd {
	return func() tea.Msg {
		if err := vaultops.UnlockRecovery(vaultEnv("", socketPath), mnemonic); err != nil {
			return vaultOpResultMsg{err: vaultStatusErr(err)}
		}
		return vaultOpResultMsg{status: "vault unlocked with recovery phrase"}
	}
}

// vaultLockCmd locks the vault agent immediately; no passphrase needed since
// locking just wipes the in-memory master key (mirrors lockVaultCmd in
// agent.go, which serves keys-mode agents too and so cannot share this path).
func vaultLockCmd(socketPath string) tea.Cmd {
	return func() tea.Msg {
		if err := vaultops.Lock(vaultEnv("", socketPath)); err != nil {
			return vaultOpResultMsg{err: vaultStatusErr(err)}
		}
		return vaultOpResultMsg{status: "vault locked"}
	}
}
