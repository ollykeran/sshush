package tui

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/openssh"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/utils"
	"golang.org/x/crypto/ssh"
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
	agentFocusFilePicker
	agentFocusPassphrase
)

type AgentScreen struct {
	keyTable     KeyTable
	buttons      ButtonRow
	configPath   string
	socketPath   string
	status       string
	statusErr    bool
	daemonRunning bool
	width        int
	height       int

	foundKeys      []string
	foundSelected  int
	loadedFPs      map[string]bool

	filePicker   StyledFilePicker
	showPicker   bool

	passInput    textinput.Model
	showPass     bool
	passAction   string // "lock" or "unlock"

	focus int
}

func NewAgentScreen(configPath, socketPath string) *AgentScreen {
	pi := textinput.New()
	pi.Placeholder = "passphrase"
	pi.EchoMode = textinput.EchoPassword
	pi.EchoCharacter = '*'

	btns := NewButtonRow("Start", "Stop", "Reload")
	btns.Focused = true

	return &AgentScreen{
		keyTable:   NewKeyTable(80, 8),
		buttons:    btns,
		configPath: configPath,
		socketPath: socketPath,
		status:     "loading...",
		loadedFPs:  make(map[string]bool),
		filePicker: NewStyledFilePicker(false),
		passInput:  pi,
		focus:      agentFocusButtons,
	}
}

func (s *AgentScreen) Init() tea.Cmd {
	return tea.Batch(
		fetchAgentKeysCmd(s.socketPath, false),
		checkDaemonCmd(s.socketPath),
		discoverKeysCmd(s.configPath),
	)
}

func (s *AgentScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
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
		s.keyTable.SetSize(s.width, tableH)
		s.filePicker.SetHeight(tableH)
		return s, nil

	case agentDaemonStateMsg:
		s.daemonRunning = msg.running
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
		s.focus = agentFocusButtons
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
		s.focus = agentFocusButtons
		return s, fetchAgentKeysCmd(s.socketPath, true)

	case tea.KeyPressMsg:
		if s.showPass {
			return s.handlePassInput(msg)
		}
		if s.showPicker {
			return s.handleFilePicker(msg)
		}
		return s.handleKeys(msg)
	}

	if s.focus == agentFocusTable {
		cmd := s.keyTable.Update(msg)
		return s, cmd
	}
	return s, nil
}

func (s *AgentScreen) handleKeys(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		return s, tea.Quit

	case "up", "k":
		switch s.focus {
		case agentFocusButtons:
			return s, navToTabBarCmd()
		case agentFocusTable:
			s.focus = agentFocusButtons
			s.buttons.Focused = true
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
		case agentFocusButtons:
			s.buttons.Focused = false
			s.focus = agentFocusTable
		case agentFocusTable:
			visible := s.visibleFoundKeys()
			if len(visible) > 0 {
				s.focus = agentFocusFound
				s.foundSelected = 0
			} else {
				s.focus = agentFocusButtons
				s.buttons.Focused = true
			}
		case agentFocusFound:
			if s.foundSelected < len(s.visibleFoundKeys())-1 {
				s.foundSelected++
			} else {
				s.focus = agentFocusButtons
				s.buttons.Focused = true
			}
		}
		return s, nil

	case "left", "h":
		if s.focus == agentFocusButtons {
			s.buttons.Left()
		}
		return s, nil

	case "right", "l":
		if s.focus == agentFocusButtons {
			s.buttons.Right()
		}
		return s, nil

	case "enter":
		switch s.focus {
		case agentFocusButtons:
			return s.pressButton(s.buttons.Active)
		case agentFocusFound:
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
		s.showPicker = true
		s.focus = agentFocusFilePicker
		return s, s.filePicker.Init()

	case "d":
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

func (s *AgentScreen) handlePassInput(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.showPass = false
		s.passInput.Blur()
		s.focus = agentFocusButtons
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

func (s *AgentScreen) handleFilePicker(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	if msg.String() == "esc" {
		s.showPicker = false
		s.focus = agentFocusButtons
		return s, nil
	}

	cmd := s.filePicker.Update(msg)
	if didSelect, path := s.filePicker.DidSelectFile(msg); didSelect {
		s.showPicker = false
		s.focus = agentFocusButtons
		return s, addKeyToAgentCmd(s.socketPath, path)
	}
	return s, cmd
}

func (s *AgentScreen) pressButton(btn int) (Screen, tea.Cmd) {
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

func (s *AgentScreen) removeSelectedKey() (Screen, tea.Cmd) {
	row := s.keyTable.SelectedRow()
	if row == nil {
		return s, nil
	}
	fp := row[1]
	return s, removeKeyFromAgentCmd(s.socketPath, fp)
}

func (s *AgentScreen) addFoundKey() (Screen, tea.Cmd) {
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

func (s *AgentScreen) View(width, height int, active bool) string {
	if s.showPicker {
		title := SectionTitleStyle.Render("Select key file")
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			title+"\n"+FocusedBorderStyle.Render(s.filePicker.View()))
	}

	if s.showPass {
		title := SectionTitleStyle.Render("Enter " + s.passAction + " passphrase")
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			title+"\n"+FocusedBorderStyle.Render(s.passInput.View()))
	}

	w := width
	if w < 1 {
		w = 80
	}

	statusBar := s.renderStatusBar(w, active)
	keyBox := s.keyTable.FocusedBoxView(active && s.focus == agentFocusTable)

	var sections []string
	sections = append(sections, statusBar)
	sections = append(sections, "")
	sections = append(sections, lipgloss.Place(w, 0, lipgloss.Center, lipgloss.Top, keyBox))

	visible := s.visibleFoundKeys()
	if len(visible) > 0 {
		sections = append(sections, "")
		foundContent := s.renderFoundKeys(visible, w, active)
		sections = append(sections, foundContent)
	}

	content := strings.Join(sections, "\n")
	return content
}

func (s *AgentScreen) renderStatusBar(w int, active bool) string {
	title := TitleStyle.Render("sshushd")
	sep := DimStyle.Render(" | ")

	var state string
	if s.daemonRunning {
		state = GreenStyle.Render("running")
	} else {
		state = ErrorStyle.Render("stopped")
	}

	statusStyle := PinkStyle
	if s.statusErr {
		statusStyle = ErrorStyle
	}
	status := statusStyle.Render(s.status)

	left := " " + title + sep + state + sep + status
	savedFocused := s.buttons.Focused
	if !active {
		s.buttons.Focused = false
	}
	buttons := s.buttons.View()
	s.buttons.Focused = savedFocused
	right := buttons

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := w - leftW - rightW
	if gap < 1 {
		gap = 1
	}

	return left + strings.Repeat(" ", gap) + right
}

func (s *AgentScreen) renderFoundKeys(visible []string, width int, active bool) string {
	title := SectionTitleStyle.Render(" Found Keys")
	var lines []string
	maxShow := 6
	if len(visible) < maxShow {
		maxShow = len(visible)
	}
	for i := 0; i < maxShow; i++ {
		style := PinkStyle
		prefix := "  "
		if active && s.focus == agentFocusFound && i == s.foundSelected {
			style = lipgloss.NewStyle().Foreground(ColorBlack).Background(ColorGreen).Bold(true)
			prefix = "> "
		}
		lines = append(lines, style.Render(prefix+visible[i]))
	}
	if len(visible) > maxShow {
		lines = append(lines, DimStyle.Render(fmt.Sprintf("  ... and %d more", len(visible)-maxShow)))
	}
	content := strings.Join(lines, "\n")
	boxW := width * 3 / 4
	if boxW > 120 {
		boxW = 120
	}
	border := UnfocusedBorderStyle
	if active && s.focus == agentFocusFound {
		border = FocusedBorderStyle
	}
	return lipgloss.Place(width, 0, lipgloss.Center, lipgloss.Top,
		title+"\n"+border.Width(boxW-4).Render(content))
}

func (s *AgentScreen) HelpEntries() []string {
	return []string{
		HelpRow("s", "Start daemon"),
		HelpRow("x", "Stop daemon"),
		HelpRow("r", "Reload daemon"),
		"",
		HelpRow("a", "Add key"),
		HelpRow("d", "Remove key"),
		HelpRow("L", "Lock agent"),
		HelpRow("U", "Unlock agent"),
		"",
		HelpRow("left/h", "Previous button"),
		HelpRow("right/l", "Next button"),
		HelpRow("up/k", "Move up"),
		HelpRow("down/j", "Move down"),
		HelpRow("enter", "Activate"),
		"",
		HelpRow("q/esc", "Quit"),
	}
}

// Commands

func fetchAgentKeysCmd(socketPath string, refresh bool) tea.Cmd {
	return func() tea.Msg {
		if socketPath == "" {
			return agentKeysMsg{err: fmt.Errorf("no socket path configured")}
		}
		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			return agentKeysMsg{err: fmt.Errorf("agent not running")}
		}
		defer conn.Close()
		keys, err := sshagent.NewClient(conn).List()
		if err != nil {
			return agentKeysMsg{err: err}
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
		if sshushd.CheckAlreadyRunning(socketPath) {
			return agentStatusMsg{text: "already running"}
		}
		sshushdPath, err := findSshushdBinary()
		if err != nil {
			return agentStatusMsg{text: "sshushd binary not found", isErr: true}
		}
		child := exec.Command(sshushdPath)
		if configPath != "" {
			child.Env = append(os.Environ(), "SSHUSH_CONFIG="+configPath)
		}
		child.Stdin = nil
		child.Stdout = nil
		child.Stderr = nil
		if err := child.Start(); err != nil {
			return agentStatusMsg{text: "start failed: " + err.Error(), isErr: true}
		}
		if !sshushd.WaitForSocket(socketPath, 50, 10*time.Millisecond) {
			return agentStatusMsg{text: "started but socket not ready", isErr: true}
		}
		return agentStatusMsg{text: "started"}
	}
}

func stopDaemonCmd() tea.Cmd {
	return func() tea.Msg {
		pidFilePath := utils.PidFilePath()
		if _, err := os.Stat(pidFilePath); os.IsNotExist(err) {
			return agentStatusMsg{text: "not running", isErr: true}
		}
		if err := stopDaemon(pidFilePath); err != nil {
			return agentStatusMsg{text: "stop failed", isErr: true}
		}
		return agentStatusMsg{text: "stopped"}
	}
}

func reloadDaemonCmd(configPath, socketPath string) tea.Cmd {
	return func() tea.Msg {
		pidFilePath := utils.PidFilePath()
		_ = stopDaemon(pidFilePath)
		time.Sleep(100 * time.Millisecond)

		sshushdPath, err := findSshushdBinary()
		if err != nil {
			return agentStatusMsg{text: "sshushd binary not found", isErr: true}
		}
		child := exec.Command(sshushdPath)
		if configPath != "" {
			child.Env = append(os.Environ(), "SSHUSH_CONFIG="+configPath)
		}
		child.Stdin = nil
		child.Stdout = nil
		child.Stderr = nil
		if err := child.Start(); err != nil {
			return agentStatusMsg{text: "reload failed: " + err.Error(), isErr: true}
		}
		if !sshushd.WaitForSocket(socketPath, 50, 10*time.Millisecond) {
			return agentStatusMsg{text: "reload: socket not ready", isErr: true}
		}
		return agentStatusMsg{text: "reloaded"}
	}
}

func removeKeyFromAgentCmd(socketPath, fingerprint string) tea.Cmd {
	return func() tea.Msg {
		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			return agentStatusMsg{text: "agent not running", isErr: true}
		}
		defer conn.Close()
		client := sshagent.NewClient(conn)
		keys, err := client.List()
		if err != nil {
			return agentStatusMsg{text: err.Error(), isErr: true}
		}
		for _, k := range keys {
			if ssh.FingerprintSHA256(k) == fingerprint {
				if err := client.Remove(k); err != nil {
					return agentStatusMsg{text: "remove failed: " + err.Error(), isErr: true}
				}
				return agentStatusMsg{text: "key removed"}
			}
		}
		return agentStatusMsg{text: "key not found", isErr: true}
	}
}

func addKeyToAgentCmd(socketPath, path string) tea.Cmd {
	return func() tea.Msg {
		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			return agentStatusMsg{text: "agent not running", isErr: true}
		}
		defer conn.Close()
		keyring := sshagent.NewClient(conn)
		if err := agent.AddKeyFromPath(keyring, path); err != nil {
			return agentStatusMsg{text: "add failed: " + err.Error(), isErr: true}
		}
		return agentStatusMsg{text: "key added: " + filepath.Base(path)}
	}
}

func lockAgentCmd(socketPath, passphrase string) tea.Cmd {
	return func() tea.Msg {
		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			return agentLockResultMsg{err: fmt.Errorf("agent not running")}
		}
		defer conn.Close()
		client := sshagent.NewClient(conn)
		err = client.Lock([]byte(passphrase))
		return agentLockResultMsg{err: err}
	}
}

func unlockAgentCmd(socketPath, passphrase string) tea.Cmd {
	return func() tea.Msg {
		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			return agentUnlockResultMsg{err: fmt.Errorf("agent not running")}
		}
		defer conn.Close()
		client := sshagent.NewClient(conn)
		err = client.Unlock([]byte(passphrase))
		return agentUnlockResultMsg{err: err}
	}
}

func discoverKeysCmd(configPath string) tea.Cmd {
	return func() tea.Msg {
		seen := make(map[string]bool)
		var paths []string

		addPath := func(p string) {
			abs, err := filepath.Abs(p)
			if err != nil {
				abs = p
			}
			if seen[abs] {
				return
			}
			if _, err := os.Stat(abs); err != nil {
				return
			}
			seen[abs] = true
			paths = append(paths, abs)
		}

		if configPath != "" {
			cfg, err := config.LoadConfig(configPath)
			if err == nil {
				for _, p := range cfg.KeyPaths {
					addPath(p)
				}
			}
		}

		home, err := os.UserHomeDir()
		if err == nil {
			sshDir := filepath.Join(home, ".ssh")
			matches, _ := filepath.Glob(filepath.Join(sshDir, "id_*"))
			for _, m := range matches {
				if strings.HasSuffix(m, ".pub") {
					continue
				}
				addPath(m)
			}

			entries, _ := os.ReadDir(sshDir)
			for _, e := range entries {
				if e.IsDir() || strings.HasSuffix(e.Name(), ".pub") {
					continue
				}
				path := filepath.Join(sshDir, e.Name())
				if seen[path] {
					continue
				}
				data, err := os.ReadFile(path)
				if err != nil || len(data) == 0 {
					continue
				}
				if _, err := openssh.ParsePrivateKeyBlob(data); err == nil {
					addPath(path)
				}
			}
		}

		return foundKeysMsg{paths: paths}
	}
}

// Shared helpers used by daemon commands. These duplicate the cli package's
// helpers to avoid an import cycle. They're small enough that duplication is
// preferable to a shared package.

func findSshushdBinary() (string, error) {
	self, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(self), "sshushd")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return exec.LookPath("sshushd")
}

func stopDaemon(pidFilePath string) error {
	data, err := os.ReadFile(pidFilePath)
	if err != nil {
		return err
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err != nil {
		return err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(os.Interrupt); err != nil {
		return err
	}
	_ = os.Remove(pidFilePath)
	return nil
}
