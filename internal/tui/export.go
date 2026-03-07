package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/keys"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

type exportKeyLoadedMsg struct {
	pubKeyStr  string
	keyType    string
	sourcePath string
	err        error
}

type exportAgentKeysMsg struct {
	keys []*sshagent.Key
	err  error
}

type exportCopyMsg struct {
	err error
}

type exportSaveMsg struct {
	err error
}

const (
	exportFocusLoadFile = iota
	exportFocusLoadAgent
	exportFocusAgentTable
	exportFocusPubKey
	exportFocusCopy
	exportFocusSaveFile
)

// ExportScreen is the export tab for viewing, copying, and saving public keys.
type ExportScreen struct {
	sk           *Skeleton
	fileSelector *FileSelector

	agentKeys  KeyTable
	showAgent  bool
	socketPath string
	zonePrefix string

	pubKeyStr  string
	keyType    string
	sourcePath string

	saveFilename textinput.Model
	showSaveIn   bool

	focus     int
	width     int
	height    int
	status    string
	statusErr bool
}

// NewExportScreen creates an ExportScreen with the given skeleton and agent socket path.
func NewExportScreen(sk *Skeleton, socketPath string) *ExportScreen {
	prefix := zone.NewPrefix()

	saveIn := textinput.New()
	saveIn.Prompt = ""
	saveIn.Placeholder = "filename.pub"

	kt := NewKeyTable(80, 5)
	kt.ZonePrefix = prefix + "agent-"

	return &ExportScreen{
		sk:           sk,
		fileSelector: NewFileSelector(ModeLoadFile, "Select key file"),
		agentKeys:    kt,
		socketPath:   socketPath,
		zonePrefix:   prefix,
		saveFilename: saveIn,
		focus:        exportFocusLoadFile,
	}
}

func (s *ExportScreen) HasActiveTextInput() bool {
	return s.saveFilename.Focused()
}

func (s *ExportScreen) HasModal() bool {
	return s.fileSelector.Visible() || s.showAgent || s.showSaveIn
}

func (s *ExportScreen) Init() tea.Cmd {
	return nil
}

func (s *ExportScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if s.fileSelector.Visible() {
		switch msg.(type) {
		case tea.WindowSizeMsg, FileSelectedMsg, FilePickerCancelledMsg, exportKeyLoadedMsg, exportAgentKeysMsg, exportCopyMsg, exportSaveMsg:
			// Handle these below
		default:
			return s, s.fileSelector.Update(msg)
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.agentKeys.SetSize(s.width, 5)
		s.fileSelector.SetHeight(max(s.height-12, 8))
		return s, nil

	case FileSelectedMsg:
		s.fileSelector.Hide()
		return s, exportLoadKeyCmd(msg.Path)

	case FilePickerCancelledMsg:
		s.fileSelector.Hide()
		return s, nil

	case exportKeyLoadedMsg:
		if msg.err != nil {
			s.status = msg.err.Error()
			s.statusErr = true
			return s, nil
		}
		s.pubKeyStr = msg.pubKeyStr
		s.keyType = msg.keyType
		s.sourcePath = msg.sourcePath
		s.updateDefaultSaveFilename()
		s.status = "loaded"
		s.statusErr = false
		s.focus = exportFocusPubKey
		return s, nil

	case exportAgentKeysMsg:
		if msg.err != nil {
			s.status = msg.err.Error()
			s.statusErr = true
			return s, nil
		}
		rows := make([]table.Row, len(msg.keys))
		for i, k := range msg.keys {
			rows[i] = table.Row{k.Type(), ssh.FingerprintSHA256(k), k.Comment}
		}
		s.agentKeys.SetRows(rows)
		s.showAgent = true
		s.focus = exportFocusAgentTable
		return s, nil

	case exportCopyMsg:
		if msg.err != nil {
			s.status = "clipboard: " + msg.err.Error()
			s.statusErr = true
		} else {
			s.status = "copied to clipboard"
			s.statusErr = false
		}
		return s, nil

	case exportSaveMsg:
		if msg.err != nil {
			s.status = "save failed: " + msg.err.Error()
			s.statusErr = true
		} else {
			s.status = "saved"
			s.statusErr = false
		}
		s.showSaveIn = false
		return s, nil

	case tea.MouseReleaseMsg:
		if msg.Button != tea.MouseLeft || s.fileSelector.Visible() || s.showAgent || s.showSaveIn {
			return s, nil
		}
		return s.handleMouse(msg.X, msg.Y)

	case tea.KeyPressMsg:
		if s.fileSelector.Visible() {
			return s, s.fileSelector.Update(msg)
		}
		if s.showAgent && s.focus == exportFocusAgentTable {
			return s.handleAgentTable(msg)
		}
		if s.showSaveIn {
			return s.handleSaveInput(msg)
		}
		return s.handleKeys(msg)
	}
	return s, nil
}

func (s *ExportScreen) handleMouse(x, y int) (tea.Model, tea.Cmd) {
	if inZoneBounds(s.zonePrefix+"load-file", x, y) {
		s.focus = exportFocusLoadFile
		return s, s.fileSelector.Show()
	}
	if inZoneBounds(s.zonePrefix+"load-agent", x, y) {
		s.focus = exportFocusLoadAgent
		return s, exportFetchAgentKeysCmd(s.socketPath)
	}
	if s.pubKeyStr != "" {
		if inZoneBounds(s.zonePrefix+"copy", x, y) {
			s.focus = exportFocusCopy
			return s, copyToClipboardCmd(s.pubKeyStr)
		}
		if inZoneBounds(s.zonePrefix+"save", x, y) {
			s.focus = exportFocusSaveFile
			s.showSaveIn = true
			return s, s.saveFilename.Focus()
		}
	}
	return s, nil
}

func (s *ExportScreen) handleKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		return s, tea.Quit
	case "down", "j":
		s.advanceFocus(1)
		return s, nil
	case "up", "k":
		cmd := s.advanceFocus(-1)
		return s, cmd
	case "left", "h", "right", "l":
		return s, nil
	case "enter":
		switch s.focus {
		case exportFocusLoadFile:
			return s, s.fileSelector.Show()
		case exportFocusLoadAgent:
			return s, exportFetchAgentKeysCmd(s.socketPath)
		case exportFocusCopy:
			return s, copyToClipboardCmd(s.pubKeyStr)
		case exportFocusSaveFile:
			s.showSaveIn = true
			return s, s.saveFilename.Focus()
		}
	}
	return s, nil
}

func (s *ExportScreen) handleAgentTable(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.showAgent = false
		s.focus = exportFocusLoadFile
		return s, nil
	case "enter":
		row := s.agentKeys.SelectedRow()
		if row != nil {
			pubKeyLine := row[0] + " " + row[1] + " " + row[2]
			s.pubKeyStr = pubKeyLine
			s.keyType = row[0]
			s.sourcePath = ""
			s.updateDefaultSaveFilename()
			s.showAgent = false
			s.focus = exportFocusPubKey
			s.status = "loaded from agent"
			s.statusErr = false
		}
		return s, nil
	}
	cmd := s.agentKeys.Update(msg)
	return s, cmd
}

func (s *ExportScreen) handleSaveInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.showSaveIn = false
		s.saveFilename.Blur()
		return s, nil
	case "enter":
		s.saveFilename.Blur()
		filename := s.saveFilename.Value()
		if filename == "" {
			s.status = "no filename"
			s.statusErr = true
			s.showSaveIn = false
			return s, nil
		}
		return s, exportSavePubKeyCmd(s.pubKeyStr, filename)
	}
	var cmd tea.Cmd
	s.saveFilename, cmd = s.saveFilename.Update(msg)
	return s, cmd
}

func (s *ExportScreen) advanceFocus(dir int) tea.Cmd {
	next := s.focus + dir
	if next < exportFocusLoadFile {
		return navToTabBarCmd()
	}
	max := exportFocusLoadAgent
	if s.pubKeyStr != "" {
		max = exportFocusSaveFile
	}
	if next > max {
		next = max
	}
	// Skip agent table focus when not showing
	if next == exportFocusAgentTable && !s.showAgent {
		next += dir
		if next < exportFocusLoadFile {
			return navToTabBarCmd()
		}
		if next > max {
			next = max
		}
	}
	s.focus = next
	return nil
}

func (s *ExportScreen) updateDefaultSaveFilename() {
	if s.sourcePath != "" {
		base := filepath.Base(s.sourcePath)
		if !strings.HasSuffix(base, ".pub") {
			base += ".pub"
		}
		s.saveFilename.SetValue(base)
	} else if s.keyType != "" {
		typeName := strings.TrimPrefix(s.keyType, "ssh-")
		s.saveFilename.SetValue("id_" + typeName + ".pub")
	}
}

func (s *ExportScreen) View() tea.View {
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
			s.fileSelector.View(width, height, active)))
	}

	if s.showAgent {
		title := SectionTitleStyle.Render("Select key from agent")
		return tea.NewView(lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			title+"\n"+s.agentKeys.FocusedBoxView(true)))
	}

	w := width
	if w < 1 {
		w = 80
	}

	var sections []string

	loadFileFocused := active && s.focus == exportFocusLoadFile
	loadAgentFocused := active && s.focus == exportFocusLoadAgent
	loadFileStyle := PinkStyle
	loadAgentStyle := PinkStyle
	loadFileLabel := "  Load from file"
	loadAgentLabel := "  Load from agent"
	if loadFileFocused {
		loadFileStyle = lipgloss.NewStyle().Foreground(ColorBlack).Background(ColorGreen).Bold(true)
		loadFileLabel = "> Load from file"
	}
	if loadAgentFocused {
		loadAgentStyle = lipgloss.NewStyle().Foreground(ColorBlack).Background(ColorGreen).Bold(true)
		loadAgentLabel = "> Load from agent"
	}
	sections = append(sections,
		zone.Mark(s.zonePrefix+"load-file", loadFileStyle.Render(loadFileLabel)),
		zone.Mark(s.zonePrefix+"load-agent", loadAgentStyle.Render(loadAgentLabel)),
	)

	if s.pubKeyStr != "" {
		sections = append(sections, "")

		contentW := w * 3 / 4
		if contentW > 100 {
			contentW = 100
		}

		pubStyle := PinkStyle
		if active && s.focus == exportFocusPubKey {
			pubStyle = lipgloss.NewStyle().Foreground(ColorBright).Background(ColorGreen)
		}
		sections = append(sections, SectionBox("Public Key", pubStyle.Render(s.pubKeyStr), contentW, active && s.focus == exportFocusPubKey))

		copyFocused := active && s.focus == exportFocusCopy
		copyStyle := PinkStyle
		copyLabel := "  Copy to clipboard"
		if copyFocused {
			copyStyle = lipgloss.NewStyle().Foreground(ColorBlack).Background(ColorGreen).Bold(true)
			copyLabel = "> Copy to clipboard"
		}
		sections = append(sections, zone.Mark(s.zonePrefix+"copy", copyStyle.Render(copyLabel)))

		saveFocused := active && s.focus == exportFocusSaveFile
		saveStyle := PinkStyle
		saveLabel := "  Save to file"
		if saveFocused {
			saveStyle = lipgloss.NewStyle().Foreground(ColorBlack).Background(ColorGreen).Bold(true)
			saveLabel = "> Save to file"
		}
		sections = append(sections, zone.Mark(s.zonePrefix+"save", saveStyle.Render(saveLabel)))

		if s.showSaveIn {
			sections = append(sections, SectionBox("Filename", s.saveFilename.View(), contentW, active))
		}
	}

	if s.status != "" {
		style := GreenStyle
		if s.statusErr {
			style = ErrorStyle
		}
		sections = append(sections, "", style.Render("  "+s.status))
	}

	content := strings.Join(sections, "\n")
	return tea.NewView(lipgloss.Place(w, height, lipgloss.Center, lipgloss.Top,
		lipgloss.NewStyle().Padding(1, 2).Render(content)))
}

func (s *ExportScreen) HelpEntries() []string {
	return []string{
		HelpRow("up/k", "Previous field"),
		HelpRow("down/j", "Next field"),
		HelpRow("enter", "Activate"),
		"",
	}
}

func (s *ExportScreen) StatusTextRaw() (string, bool) {
	return s.status, s.statusErr
}

// Commands

func exportLoadKeyCmd(path string) tea.Cmd {
	return func() tea.Msg {
		parsed, _, signer, err := keys.LoadKeyMaterial(path)
		if err != nil {
			if strings.Contains(err.Error(), "encrypted keys not supported") {
				return exportKeyLoadedMsg{err: fmt.Errorf("not an unencrypted OpenSSH key")}
			}
			return exportKeyLoadedMsg{err: err}
		}

		return exportKeyLoadedMsg{
			pubKeyStr:  strings.TrimSpace(keys.FormatPublicKey(signer, parsed.Comment)),
			keyType:    parsed.KeyType,
			sourcePath: path,
		}
	}
}

func exportFetchAgentKeysCmd(socketPath string) tea.Cmd {
	return func() tea.Msg {
		if socketPath == "" {
			return exportAgentKeysMsg{err: fmt.Errorf("no socket path")}
		}
		keys, err := agent.ListKeysFromSocket(socketPath)
		if err != nil {
			return exportAgentKeysMsg{err: fmt.Errorf("agent not running")}
		}
		return exportAgentKeysMsg{keys: keys}
	}
}

func copyToClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		err := copyToClipboard(text)
		return exportCopyMsg{err: err}
	}
}

func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			cmd = exec.Command("wl-copy")
		} else {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		}
	case "darwin":
		cmd = exec.Command("pbcopy")
	default:
		return fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func exportSavePubKeyCmd(pubKeyStr, filename string) tea.Cmd {
	return func() tea.Msg {
		home, err := os.UserHomeDir()
		if err != nil {
			return exportSaveMsg{err: err}
		}
		path := filepath.Join(home, ".ssh", filename)
		content := pubKeyStr + "\n"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return exportSaveMsg{err: err}
		}
		return exportSaveMsg{}
	}
}
