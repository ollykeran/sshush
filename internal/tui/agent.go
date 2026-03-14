package tui

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"

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

const (
	agentFocusButtons = iota
	agentFocusTable
	agentFocusFound
	agentFocusPassphrase
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

	focus int
}

// NewAgentScreen creates an AgentScreen with the given skeleton, config path, and socket path.
func NewAgentScreen(sk *Skeleton, configPath, socketPath string) *AgentScreen {
	prefix := zone.NewPrefix()

	pi := textinput.New()
	pi.Placeholder = "passphrase"
	pi.EchoMode = textinput.EchoPassword
	pi.EchoCharacter = '*'

	btns := NewButtonRow("[s]tart", "[x]stop", "[r]eload")
	btns.Focused = true
	btns.ZonePrefix = prefix + "ctrl-"

	kt := NewKeyTable(80, 8, sk.Styles())
	kt.ZonePrefix = prefix + "keys-"

	return &AgentScreen{
		sk:           sk,
		keyTable:     kt,
		buttons:      btns,
		zonePrefix:   prefix,
		configPath:   configPath,
		socketPath:   socketPath,
		status:       "loading...",
		loadedFPs:    make(map[string]bool),
		fileSelector: NewFileSelector(ModeLoadFile, "Select key file", sk.Styles()),
		passInput:    pi,
		focus:        agentFocusTable,
	}
}

func (s *AgentScreen) HasModal() bool {
	return s.fileSelector.Visible() || s.showPass
}

func (s *AgentScreen) Init() tea.Cmd {
	return tea.Batch(
		fetchAgentKeysCmd(s.socketPath, false),
		checkDaemonCmd(s.socketPath),
		discoverKeysCmd(),
	)
}

func (s *AgentScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if s.fileSelector.Visible() {
		switch msg.(type) {
		case tea.WindowSizeMsg, FileSelectedMsg, FilePickerCancelledMsg, agentKeysMsg, agentStatusMsg, agentDaemonStateMsg, agentLockResultMsg, agentUnlockResultMsg, foundKeysMsg, ButtonFlashDoneMsg:
			// Handle these below
		default:
			return s, s.fileSelector.Update(msg)
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		tableH := s.height - 14
		if tableH > 12 {
			tableH = 12
		}
		if tableH < 3 {
			tableH = 3
		}
		s.keyTable.SetSize(s.width, tableH, s.sk.Styles())
		s.fileSelector.SetHeight(max(s.height-12, 8))
		return s, nil

	case ThemeChangedMsg:
		tableH := s.height - 14
		if tableH > 12 {
			tableH = 12
		}
		if tableH < 3 {
			tableH = 3
		}
		s.keyTable.SetSize(s.width, tableH, s.sk.Styles())
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
			s.sk.UpdateWidgetValue("sshushd", state)
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
			s.loadedFPs = make(map[string]bool)
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
		s.statusErr = false
		if msg.refresh {
			s.status = fmt.Sprintf("refreshed %d key(s)", len(rows))
		} else if len(rows) == 0 {
			s.status = "no keys loaded"
		} else {
			s.status = fmt.Sprintf("%d key(s) loaded", len(rows))
		}
		if s.focus == agentFocusFound {
			visible := s.visibleFoundKeys()
			if len(visible) == 0 {
				s.focus = agentFocusTable
				s.foundSelected = 0
			} else if s.foundSelected >= len(visible) {
				s.foundSelected = len(visible) - 1
			}
		}
		return s, nil

	case agentStatusMsg:
		s.status = msg.text
		s.statusErr = msg.isErr
		if s.sk != nil {
			s.sk.UpdateWidgetValue("sshushd", msg.text)
		}
		if !msg.isErr {
			return s, tea.Batch(
				fetchAgentKeysCmd(s.socketPath, false),
				checkDaemonCmd(s.socketPath),
			)
		}
		return s, checkDaemonCmd(s.socketPath)

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
		}
		s.focus = agentFocusTable
		return s, fetchAgentKeysCmd(s.socketPath, true)

	case agentUnlockResultMsg:
		s.showPass = false
		s.passInput.Blur()
		if msg.err != nil {
			s.status = "unlock failed: " + msg.err.Error()
			s.statusErr = true
		} else {
			s.status = "agent unlocked"
			s.statusErr = false
		}
		s.focus = agentFocusTable
		return s, fetchAgentKeysCmd(s.socketPath, true)

	case tea.MouseReleaseMsg:
		if msg.Button != tea.MouseLeft || s.fileSelector.Visible() || s.showPass {
			return s, nil
		}
		return s.handleMouse(msg.X, msg.Y)

	case tea.KeyPressMsg:
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
			return s, navToTabBarCmd()
		case agentFocusFound:
			if s.foundSelected > 0 {
				s.foundSelected--
			} else {
				s.focus = agentFocusTable
			}
		}
		return s, nil

	case "down", "j":
		switch s.focus {
		case agentFocusTable:
			visible := s.visibleFoundKeys()
			if len(visible) > 0 {
				s.focus = agentFocusFound
				s.foundSelected = 0
			}
		case agentFocusFound:
			if s.foundSelected < len(s.visibleFoundKeys())-1 {
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

	case "L":
		s.showPass = true
		s.passAction = "lock"
		s.passInput.SetValue("")
		s.passInput.Placeholder = "lock passphrase"
		s.focus = agentFocusPassphrase
		return s, s.passInput.Focus()

	case "U":
		s.showPass = true
		s.passAction = "unlock"
		s.passInput.SetValue("")
		s.passInput.Placeholder = "unlock passphrase"
		s.focus = agentFocusPassphrase
		return s, s.passInput.Focus()
	}

	if s.focus == agentFocusTable {
		cmd := s.keyTable.Update(msg)
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
	if row := s.keyTable.HandleMouse(x, y); row >= 0 {
		s.focus = agentFocusTable
		s.keyTable.Table.SetCursor(row)
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
	s.buttons.Press()
	s.statusErr = false

	var action tea.Cmd
	switch btn {
	case 0: // Start
		s.status = "starting..."
		action = startDaemonCmd(s.configPath, s.socketPath)
	case 1: // Stop
		s.status = "stopping..."
		action = stopDaemonCmd()
	case 2: // Reload
		s.status = "reloading..."
		action = reloadDaemonCmd(s.configPath, s.socketPath)
	}
	return s, tea.Batch(action, ButtonFlashCmd())
}

func (s *AgentScreen) removeSelectedKey() (tea.Model, tea.Cmd) {
	row := s.keyTable.SelectedRow()
	if row == nil {
		return s, nil
	}
	fp := row[1]
	return s, removeKeyFromAgentCmd(s.socketPath, fp)
}

func (s *AgentScreen) addFoundKey() (tea.Model, tea.Cmd) {
	visible := s.visibleFoundKeys()
	if s.foundSelected >= len(visible) {
		return s, nil
	}
	path := visible[s.foundSelected]
	return s, addKeyToAgentCmd(s.socketPath, path)
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
	width := 80
	height := 24
	if s.sk != nil {
		width = s.sk.GetTerminalWidth()
		height = s.sk.GetTerminalHeight() - 12
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

	w := width
	if w < 1 {
		w = 80
	}

	st := s.sk.Styles()
	keyBox := s.keyTable.FocusedBoxView(st, active && s.focus == agentFocusTable)

	var sections []string
	sections = append(sections, lipgloss.Place(w, 0, lipgloss.Center, lipgloss.Top, keyBox))

	visible := s.visibleFoundKeys()
	if len(visible) > 0 {
		sections = append(sections, "")
		foundContent := s.renderFoundKeys(visible, w, active)
		sections = append(sections, foundContent)
	}

	content := strings.Join(sections, "\n")
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

func (s *AgentScreen) renderFoundKeys(visible []string, width int, active bool) string {
	st := s.sk.Styles()
	title := st.SectionTitleStyle.Render(" Found Keys")
	var lines []string
	maxShow := 6
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
		rendered := style.Render(linePrefix + visible[i])
		rendered = zone.Mark(fmt.Sprintf("%sfound-%d", s.zonePrefix, i), rendered)
		lines = append(lines, rendered)
	}
	if len(visible) > maxShow {
		lines = append(lines, st.DimStyle.Render(fmt.Sprintf("  ... and %d more", len(visible)-maxShow)))
	}
	content := strings.Join(lines, "\n")
	boxW := width * 3 / 4
	if boxW > 120 {
		boxW = 120
	}
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
		st.HelpRow("d / bksp", "Remove key"),
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
		if err := agent.AddKeyToSocketFromPath(socketPath, path); err != nil {
			return agentStatusMsg{text: "add failed: " + err.Error(), isErr: true}
		}
		return agentStatusMsg{text: "key added: " + filepath.Base(path)}
	}
}

func lockAgentCmd(socketPath, passphrase string) tea.Cmd {
	return func() tea.Msg {
		return agentLockResultMsg{err: agent.LockSocket(socketPath, []byte(passphrase))}
	}
}

func unlockAgentCmd(socketPath, passphrase string) tea.Cmd {
	return func() tea.Msg {
		return agentUnlockResultMsg{err: agent.UnlockSocket(socketPath, []byte(passphrase))}
	}
}

func discoverKeysCmd() tea.Cmd {
	return func() tea.Msg {
		return foundKeysMsg{paths: utils.DiscoverKeyPaths([]string{}, true, true, false)}
	}
}
