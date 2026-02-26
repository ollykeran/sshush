package cli

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

var (
	tuiGreen  = lipgloss.Color("#7EE787")
	tuiPink   = lipgloss.Color("#F472B6")
	tuiPurple = lipgloss.Color("#631596")
	tuiErr    = lipgloss.Color("#F87171")
)

const (
	btnStart  = 0
	btnStop   = 1
	btnReload = 2
	btnCount  = 3
)

var btnLabels = [btnCount]string{"Start", "Stop", "Reload"}

type keysMsg struct {
	keys    []*agent.Key
	err     error
	refresh bool
}

type statusMsg struct {
	text  string
	isErr bool
}

type daemonStateMsg struct {
	running bool
}

type buttonFlashDoneMsg struct{}

type tuiModel struct {
	table         table.Model
	configPath    string
	socketPath    string
	status        string
	statusErr     bool
	showHelp      bool
	width         int
	height        int
	quitting      bool
	daemonRunning bool
	activeButton  int
	pressedButton int
}

func newTUICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Start the sshush TUI",
		RunE:  runTUI,
	}
}

func runTUI(cmd *cobra.Command, _ []string) error {
	socketPath, _ := getSocketPath()
	configPath := ""
	if p, err := utils.ResolveConfigPath(cmd); err == nil {
		configPath = p
	}

	innerW := keyBoxInnerWidth(80)
	rowW := innerW + tuiCellPadOverhead
	t := table.New(
		table.WithColumns(tuiColumns(innerW)),
		table.WithRows([]table.Row{}),
		table.WithFocused(true),
		table.WithHeight(10),
		table.WithWidth(rowW),
	)

	t.SetStyles(tuiTableStyles(rowW))

	m := tuiModel{
		table:         t,
		configPath:    configPath,
		socketPath:    socketPath,
		status:        "loading...",
		pressedButton: -1,
	}

	_, err := tea.NewProgram(m).Run()
	return err
}

const tuiCellPadOverhead = 6 // 3 columns * 2 chars padding each

func tuiTableStyles(rowWidth int) table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true).
		Foreground(tuiGreen).
		Padding(0, 1)
	s.Cell = s.Cell.
		Foreground(tuiPink).
		Padding(0, 1)
	s.Selected = lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).
		Background(tuiPurple).
		Bold(true).
		Width(rowWidth)
	return s
}

func keyBoxInnerWidth(termWidth int) int {
	boxW := termWidth * 3 / 4
	if boxW > 120 {
		boxW = 120
	}
	if boxW < 106 {
		boxW = 106
	}
	return boxW - 4
}

func tuiColumns(w int) []table.Column {
	if w < 36 {
		w = 36
	}
	typeW := 11
	fpW := 51
	commentW := w - typeW - fpW
	if commentW < 40 {
		commentW = 40
		fpW = w - typeW - commentW
		if fpW < 51 {
			fpW = 51
		}
	}
	return []table.Column{
		{Title: "Type", Width: typeW},
		{Title: "Fingerprint", Width: fpW},
		{Title: "Comment", Width: commentW},
	}
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(
		fetchKeysCmd(m.socketPath, false),
		checkDaemonCmd(m.socketPath),
	)
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		innerW := keyBoxInnerWidth(m.width)
		rowW := innerW + tuiCellPadOverhead
		m.table.SetColumns(tuiColumns(innerW))
		m.table.SetWidth(rowW)
		m.table.SetStyles(tuiTableStyles(rowW))

		contentH := m.height - 2
		tableH := contentH - 8
		if tableH > 12 {
			tableH = 12
		}
		if tableH < 3 {
			tableH = 3
		}
		m.table.SetHeight(tableH)
		return m, nil

	case daemonStateMsg:
		m.daemonRunning = msg.running
		return m, nil

	case buttonFlashDoneMsg:
		m.pressedButton = -1
		return m, nil

	case keysMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
			m.statusErr = true
			m.table.SetRows([]table.Row{})
			return m, nil
		}
		rows := make([]table.Row, len(msg.keys))
		for i, k := range msg.keys {
			rows[i] = table.Row{k.Type(), ssh.FingerprintSHA256(k), k.Comment}
		}
		m.table.SetRows(rows)
		m.statusErr = false
		if msg.refresh {
			m.status = fmt.Sprintf("refreshed %d key(s)", len(rows))
		} else if len(rows) == 0 {
			m.status = "no keys loaded"
		} else {
			m.status = fmt.Sprintf("%d key(s) loaded", len(rows))
		}
		return m, nil

	case statusMsg:
		m.status = msg.text
		m.statusErr = msg.isErr
		if !msg.isErr {
			return m, tea.Batch(
				fetchKeysCmd(m.socketPath, false),
				checkDaemonCmd(m.socketPath),
			)
		}
		return m, checkDaemonCmd(m.socketPath)

	case tea.KeyPressMsg:
		if m.showHelp {
			switch msg.String() {
			case "ctrl+c":
				m.quitting = true
				return m, tea.Quit
			case "q", "esc":
				m.showHelp = false
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "?":
			m.showHelp = true
			return m, nil
		case "left", "h":
			m.activeButton = (m.activeButton - 1 + btnCount) % btnCount
			return m, nil
		case "right", "l":
			m.activeButton = (m.activeButton + 1) % btnCount
			return m, nil
		case "enter":
			return m.pressButton(m.activeButton)
		case "s":
			return m.pressButton(btnStart)
		case "x":
			return m.pressButton(btnStop)
		case "r":
			return m.pressButton(btnReload)
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m tuiModel) pressButton(btn int) (tea.Model, tea.Cmd) {
	m.activeButton = btn
	m.pressedButton = btn
	m.statusErr = false

	var action tea.Cmd
	switch btn {
	case btnStart:
		m.status = "starting..."
		action = tuiStartDaemonCmd(m.configPath, m.socketPath)
	case btnStop:
		m.status = "stopping..."
		action = tuiStopDaemonCmd()
	case btnReload:
		m.status = "reloading..."
		action = tuiReloadDaemonCmd(m.configPath, m.socketPath)
	}

	return m, tea.Batch(action, buttonFlashCmd())
}

// view

func (m tuiModel) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	if m.showHelp {
		v := tea.NewView(tuiHelpView(m.width, m.height))
		v.AltScreen = true
		return v
	}

	w := m.width
	if w < 1 {
		w = 80
	}
	h := m.height
	if h < 1 {
		h = 24
	}

	statusBar := m.renderStatusBar(w)

	tableView := m.table.View()
	keyBox := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(tuiPurple).
		Padding(0, 1).
		Render(tableView)

	contentH := h - 2
	if contentH < 5 {
		contentH = 5
	}
	centeredBox := lipgloss.Place(w, contentH, lipgloss.Center, lipgloss.Center, keyBox)

	content := statusBar + "\n" + centeredBox

	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func (m tuiModel) renderStatusBar(w int) string {
	title := lipgloss.NewStyle().Bold(true).Foreground(tuiGreen).Render("sshushd")

	sep := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(" | ")

	var state string
	if m.daemonRunning {
		state = lipgloss.NewStyle().Foreground(tuiGreen).Render("running")
	} else {
		state = lipgloss.NewStyle().Foreground(tuiErr).Render("stopped")
	}

	statusStyle := lipgloss.NewStyle().Foreground(tuiPink)
	if m.statusErr {
		statusStyle = statusStyle.Foreground(tuiErr)
	}
	status := statusStyle.Render(m.status)

	left := " " + title + sep + state + sep + status

	buttons := m.renderButtons()
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("? help ")
	right := buttons + "  " + hint

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := w - leftW - rightW
	if gap < 1 {
		gap = 1
	}

	bar := left + strings.Repeat(" ", gap) + right
	rule := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(strings.Repeat("─", w))

	return bar + "\n" + rule
}

func (m tuiModel) renderButtons() string {
	var parts []string
	for i, label := range btnLabels {
		style := lipgloss.NewStyle().Padding(0, 1)

		if m.pressedButton == i {
			style = style.
				Background(tuiGreen).
				Foreground(lipgloss.Color("#000000")).
				Bold(true)
		} else if m.activeButton == i {
			style = style.
				Background(tuiPurple).
				Foreground(lipgloss.Color("229")).
				Bold(true)
		} else {
			style = style.Foreground(tuiPink)
		}

		parts = append(parts, style.Render(label))
	}
	return strings.Join(parts, " ")
}

func tuiHelpView(w, h int) string {
	if w < 1 {
		w = 80
	}
	if h < 1 {
		h = 24
	}

	k := lipgloss.NewStyle().Foreground(tuiGreen).Bold(true).Width(14)
	d := lipgloss.NewStyle().Foreground(tuiPink)

	lines := []string{
		k.Render("s") + d.Render("Start daemon"),
		k.Render("x") + d.Render("Stop daemon"),
		k.Render("r") + d.Render("Reload daemon"),
		"",
		k.Render("left/h") + d.Render("Previous button"),
		k.Render("right/l") + d.Render("Next button"),
		k.Render("enter") + d.Render("Activate button"),
		k.Render("up/down") + d.Render("Navigate keys"),
		"",
		k.Render("?") + d.Render("Toggle help"),
		k.Render("q/esc") + d.Render("Quit"),
		"",
		lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("Press q or esc to close"),
	}

	body := ""
	for _, l := range lines {
		body += "  " + l + "\n"
	}

	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(tuiPurple).
		Padding(1, 2).
		Render(body)

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
}

// commands

func buttonFlashCmd() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(200 * time.Millisecond)
		return buttonFlashDoneMsg{}
	}
}

func checkDaemonCmd(socketPath string) tea.Cmd {
	return func() tea.Msg {
		return daemonStateMsg{running: sshushd.CheckAlreadyRunning(socketPath)}
	}
}

func fetchKeysCmd(socketPath string, refresh bool) tea.Cmd {
	return func() tea.Msg {
		if socketPath == "" {
			return keysMsg{err: fmt.Errorf("no socket path configured")}
		}
		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			return keysMsg{err: fmt.Errorf("agent not running")}
		}
		defer conn.Close()
		keys, err := agent.NewClient(conn).List()
		if err != nil {
			return keysMsg{err: err}
		}
		return keysMsg{keys: keys, refresh: refresh}
	}
}

func tuiStartDaemonCmd(configPath, socketPath string) tea.Cmd {
	return func() tea.Msg {
		if sshushd.CheckAlreadyRunning(socketPath) {
			return statusMsg{text: "already running", isErr: false}
		}
		sshushdPath, err := findSshushdBinary()
		if err != nil {
			return statusMsg{text: "sshushd binary not found", isErr: true}
		}
		child := exec.Command(sshushdPath)
		if configPath != "" {
			child.Env = append(os.Environ(), "SSHUSH_CONFIG="+configPath)
		}
		child.Stdin = nil
		child.Stdout = nil
		child.Stderr = nil
		if err := child.Start(); err != nil {
			return statusMsg{text: "start failed: " + err.Error(), isErr: true}
		}
		if !sshushd.WaitForSocket(socketPath, 50, 10*time.Millisecond) {
			return statusMsg{text: "started but socket not ready", isErr: true}
		}
		return statusMsg{text: "started", isErr: false}
	}
}

func tuiStopDaemonCmd() tea.Cmd {
	return func() tea.Msg {
		pidFilePath := utils.PidFilePath()
		if _, err := os.Stat(pidFilePath); os.IsNotExist(err) {
			return statusMsg{text: "not running", isErr: true}
		}
		if err := stopDaemon(pidFilePath); err != nil {
			return statusMsg{text: "stop failed", isErr: true}
		}
		return statusMsg{text: "stopped", isErr: false}
	}
}

func tuiReloadDaemonCmd(configPath, socketPath string) tea.Cmd {
	return func() tea.Msg {
		pidFilePath := utils.PidFilePath()
		_ = stopDaemon(pidFilePath)

		time.Sleep(100 * time.Millisecond)

		sshushdPath, err := findSshushdBinary()
		if err != nil {
			return statusMsg{text: "sshushd binary not found", isErr: true}
		}
		child := exec.Command(sshushdPath)
		if configPath != "" {
			child.Env = append(os.Environ(), "SSHUSH_CONFIG="+configPath)
		}
		child.Stdin = nil
		child.Stdout = nil
		child.Stderr = nil
		if err := child.Start(); err != nil {
			return statusMsg{text: "reload failed: " + err.Error(), isErr: true}
		}
		if !sshushd.WaitForSocket(socketPath, 50, 10*time.Millisecond) {
			return statusMsg{text: "reload: socket not ready", isErr: true}
		}
		return statusMsg{text: "reloaded", isErr: false}
	}
}
