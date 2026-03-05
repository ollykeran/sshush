package tui

import (
	"fmt"
	"path/filepath"
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

type editKeyLoadedMsg struct {
	keyType     string
	comment     string
	fingerprint string
	pubKeyStr   string
	rawKey      interface{}
	filePath    string
	err         error
}

type editSaveMsg struct {
	err error
}

type editAgentKeysMsg struct {
	keys []*sshagent.Key
	err  error
}

const (
	editFocusLoadFile = iota
	editFocusLoadAgent
	editFocusAgentTable
	editFocusComment
	editFocusSave
)

// EditScreen is the edit tab for changing key comments (load from file or agent).
type EditScreen struct {
	sk         *Skeleton
	filePicker StyledFilePicker
	showPicker bool

	agentKeys  KeyTable
	showAgent  bool
	socketPath string

	commentIn  textinput.Model
	saveBtn    ButtonRow
	zonePrefix string

	loadedPath  string
	keyType     string
	fingerprint string
	pubKeyStr   string
	rawKey      interface{}

	focus     int
	width     int
	height    int
	status    string
	statusErr bool
}

// NewEditScreen creates an EditScreen with the given skeleton and agent socket path.
func NewEditScreen(sk *Skeleton, socketPath string) *EditScreen {
	prefix := zone.NewPrefix()

	comment := textinput.New()
	comment.Prompt = ""
	comment.Placeholder = "key comment"

	kt := NewKeyTable(80, 5)
	kt.ZonePrefix = prefix + "agent-"

	saveBtn := NewButtonRow("Save")
	saveBtn.ZonePrefix = prefix + "save-"

	return &EditScreen{
		sk:         sk,
		filePicker: NewStyledFilePicker(false),
		agentKeys:  kt,
		socketPath: socketPath,
		commentIn:  comment,
		saveBtn:    saveBtn,
		zonePrefix: prefix,
		focus:      editFocusLoadFile,
	}
}

func (s *EditScreen) HasActiveTextInput() bool {
	return s.commentIn.Focused()
}

func (s *EditScreen) Init() tea.Cmd {
	return nil
}

func (s *EditScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.agentKeys.SetSize(s.width, 5)
		s.filePicker.SetHeight(s.height / 3)
		return s, nil

	case editKeyLoadedMsg:
		if msg.err != nil {
			s.status = msg.err.Error()
			s.statusErr = true
			return s, nil
		}
		s.keyType = msg.keyType
		s.fingerprint = msg.fingerprint
		s.pubKeyStr = msg.pubKeyStr
		s.rawKey = msg.rawKey
		s.loadedPath = msg.filePath
		s.commentIn.SetValue(msg.comment)
		s.status = "loaded: " + filepath.Base(msg.filePath)
		s.statusErr = false
		s.focus = editFocusComment
		return s, s.commentIn.Focus()

	case editSaveMsg:
		if msg.err != nil {
			s.status = "save failed: " + msg.err.Error()
			s.statusErr = true
		} else {
			s.status = "saved: " + filepath.Base(s.loadedPath)
			s.statusErr = false
		}
		return s, nil

	case editAgentKeysMsg:
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
		s.focus = editFocusAgentTable
		return s, nil

	case ButtonFlashDoneMsg:
		s.saveBtn.ClearPress()
		return s, nil

	case tea.MouseReleaseMsg:
		if msg.Button != tea.MouseLeft || s.showPicker || s.showAgent {
			return s, nil
		}
		return s.handleMouse(msg.X, msg.Y)

	case tea.KeyPressMsg:
		if s.showPicker {
			return s.handleFilePicker(msg)
		}
		if s.showAgent && s.focus == editFocusAgentTable {
			return s.handleAgentTable(msg)
		}
		if s.focus == editFocusComment && s.commentIn.Focused() {
			return s.handleCommentInput(msg)
		}
		return s.handleKeys(msg)
	}

	return s, nil
}

func (s *EditScreen) handleMouse(x, y int) (tea.Model, tea.Cmd) {
	if inZoneBounds(s.zonePrefix+"load-file", x, y) {
		s.focus = editFocusLoadFile
		s.showPicker = true
		return s, s.filePicker.Init()
	}
	if inZoneBounds(s.zonePrefix+"load-agent", x, y) {
		s.focus = editFocusLoadAgent
		return s, editFetchAgentKeysCmd(s.socketPath)
	}
	if s.rawKey != nil {
		if inZoneBounds(s.zonePrefix+"comment", x, y) {
			s.focus = editFocusComment
			s.saveBtn.Focused = false
			cmd := s.commentIn.Focus()
			if pos := sectionBoxCursorPos(s.zonePrefix+"comment", x, y); pos >= 0 {
				s.commentIn.SetCursor(pos)
			}
			return s, cmd
		}
		if btn := s.saveBtn.HandleMouse(x, y); btn >= 0 {
			s.commentIn.Blur()
			s.focus = editFocusSave
			s.saveBtn.Focused = true
			s.saveBtn.Press()
			comment := s.commentIn.Value()
			return s, tea.Batch(editSaveKeyCmd(s.rawKey, comment, s.loadedPath), ButtonFlashCmd())
		}
	}
	return s, nil
}

func (s *EditScreen) handleKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		return s, tea.Quit
	case "down", "j":
		return s, s.advanceFocus(1)
	case "up", "k":
		return s, s.advanceFocus(-1)
	case "left", "h", "right", "l":
		// load buttons are rendered as a single row; left/right has no effect
		return s, nil
	case "enter":
		switch s.focus {
		case editFocusLoadFile:
			s.showPicker = true
			return s, s.filePicker.Init()
		case editFocusLoadAgent:
			return s, editFetchAgentKeysCmd(s.socketPath)
		case editFocusComment:
			return s, s.commentIn.Focus()
		case editFocusSave:
			if s.rawKey == nil {
				s.status = "no key loaded"
				s.statusErr = true
				return s, nil
			}
			s.saveBtn.Press()
			comment := s.commentIn.Value()
			return s, tea.Batch(editSaveKeyCmd(s.rawKey, comment, s.loadedPath), ButtonFlashCmd())
		}
	}
	return s, nil
}

func (s *EditScreen) handleFilePicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		s.showPicker = false
		return s, nil
	}
	cmd := s.filePicker.Update(msg)
	if didSelect, path := s.filePicker.DidSelectFile(msg); didSelect {
		s.showPicker = false
		return s, editLoadKeyCmd(path)
	}
	return s, cmd
}

func (s *EditScreen) handleAgentTable(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.showAgent = false
		s.focus = editFocusLoadFile
		return s, nil
	case "enter":
		row := s.agentKeys.SelectedRow()
		if row != nil {
			s.status = "agent keys cannot be edited directly; load from file"
			s.statusErr = true
			s.showAgent = false
			s.focus = editFocusLoadFile
		}
		return s, nil
	}
	cmd := s.agentKeys.Update(msg)
	return s, cmd
}

func (s *EditScreen) handleCommentInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.commentIn.Blur()
		return s, nil
	case "tab", "down":
		return s, s.advanceFocus(1)
	case "shift+tab", "up":
		return s, s.advanceFocus(-1)
	}
	var cmd tea.Cmd
	s.commentIn, cmd = s.commentIn.Update(msg)
	return s, cmd
}

func (s *EditScreen) advanceFocus(dir int) tea.Cmd {
	s.commentIn.Blur()
	next := s.focus + dir
	if next < editFocusLoadFile {
		return navToTabBarCmd()
	}
	maxFocus := editFocusLoadAgent
	if s.rawKey != nil {
		maxFocus = editFocusSave
	}
	if next > maxFocus {
		next = maxFocus
	}
	if next == editFocusAgentTable && !s.showAgent {
		next += dir
		if next < editFocusLoadFile {
			return navToTabBarCmd()
		}
		if next > maxFocus {
			next = maxFocus
		}
	}
	s.focus = next
	s.saveBtn.Focused = s.focus == editFocusSave
	if s.focus == editFocusComment {
		return s.commentIn.Focus()
	}
	return nil
}

func (s *EditScreen) View() tea.View {
	width := 80
	height := 24
	if s.sk != nil {
		width = s.sk.GetTerminalWidth()
		height = s.sk.GetTerminalHeight() - 12
	}
	active := s.sk.ScreenActive()
	if s.showPicker {
		title := SectionTitleStyle.Render("Select private key file")
		return tea.NewView(lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			title+"\n"+FocusedBorderStyle.Render(s.filePicker.View())))
	}

	if s.showAgent {
		title := SectionTitleStyle.Render("Select key from agent")
		box := s.agentKeys.FocusedBoxView(true)
		hint := DimStyle.Render("  Note: To edit, you must load the key from its file")
		return tea.NewView(lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			title+"\n"+box+"\n"+hint))
	}

	w := width
	if w < 1 {
		w = 80
	}

	var sections []string

	loadFileFocused := active && s.focus == editFocusLoadFile
	loadAgentFocused := active && s.focus == editFocusLoadAgent
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
	loadSection := lipgloss.JoinVertical(lipgloss.Left,
		zone.Mark(s.zonePrefix+"load-file", loadFileStyle.Render(loadFileLabel)),
		zone.Mark(s.zonePrefix+"load-agent", loadAgentStyle.Render(loadAgentLabel)),
	)
	sections = append(sections, loadSection)

	if s.rawKey != nil {
		sections = append(sections, "")

		infoW := w * 3 / 4
		if infoW > 100 {
			infoW = 100
		}

		sections = append(sections, SectionBox("Public Key",
			PinkStyle.Render(truncate(s.pubKeyStr, infoW-6)), infoW, false))

		sections = append(sections, SectionBox("Fingerprint",
			PinkStyle.Render(s.fingerprint), infoW, false))

		sections = append(sections, zone.Mark(s.zonePrefix+"comment", SectionBox("Comment", s.commentIn.View(), infoW, active && s.focus == editFocusComment)))

		s.saveBtn.Focused = active && s.focus == editFocusSave
		sections = append(sections, " "+s.saveBtn.View())
	}

	if s.status != "" {
		style := GreenStyle
		if s.statusErr {
			style = ErrorStyle
		}
		sections = append(sections, style.Render("  "+s.status))
	}

	content := strings.Join(sections, "\n")
	return tea.NewView(lipgloss.Place(w, height, lipgloss.Center, lipgloss.Top,
		lipgloss.NewStyle().Padding(1, 2).Render(content)))
}

func (s *EditScreen) HelpEntries() []string {
	return []string{
		HelpRow("up/k", "Previous field"),
		HelpRow("down/j", "Next field"),
		HelpRow("enter", "Activate/Edit"),
		"",
	}
}

func (s *EditScreen) StatusTextRaw() (string, bool) {
	return s.status, s.statusErr
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen || maxLen < 4 {
		return s
	}
	return s[:maxLen-3] + "..."
}

// Commands

func editLoadKeyCmd(path string) tea.Cmd {
	return func() tea.Msg {
		parsed, rawKey, signer, err := keys.LoadKeyMaterial(path)
		if err != nil {
			if strings.Contains(err.Error(), "encrypted keys not supported") {
				return editKeyLoadedMsg{err: fmt.Errorf("not an unencrypted OpenSSH key")}
			}
			return editKeyLoadedMsg{err: err}
		}

		fp := ssh.FingerprintSHA256(signer.PublicKey())

		return editKeyLoadedMsg{
			keyType:     parsed.KeyType,
			comment:     parsed.Comment,
			fingerprint: fp,
			pubKeyStr:   strings.TrimSpace(keys.FormatPublicKey(signer, parsed.Comment)),
			rawKey:      rawKey,
			filePath:    path,
		}
	}
}

func editSaveKeyCmd(rawKey interface{}, comment, filePath string) tea.Cmd {
	return func() tea.Msg {
		if err := keys.SaveWithComment(rawKey, comment, filePath); err != nil {
			return editSaveMsg{err: err}
		}

		return editSaveMsg{}
	}
}

func editFetchAgentKeysCmd(socketPath string) tea.Cmd {
	return func() tea.Msg {
		if socketPath == "" {
			return editAgentKeysMsg{err: fmt.Errorf("no socket path")}
		}
		keys, err := agent.ListKeysFromSocket(socketPath)
		if err != nil {
			return editAgentKeysMsg{err: fmt.Errorf("agent not running")}
		}
		return editAgentKeysMsg{keys: keys}
	}
}
