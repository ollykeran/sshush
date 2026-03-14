package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/keys"
	"github.com/ollykeran/sshush/internal/utils"
	ssh "golang.org/x/crypto/ssh"
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

const (
	editFocusSelectFile = iota
	editFocusComment
	editFocusSave
)

// EditScreen is the edit tab for changing key comments.
type EditScreen struct {
	sk           *Skeleton
	fileSelector *FileSelector

	commentIn  textinput.Model
	saveBtn    ButtonRow
	zonePrefix string

	loadedPath      string
	originalComment string
	keyType         string
	fingerprint     string
	pubKeyStr       string
	rawKey          interface{}

	// saveDiffRendered is set after a successful save; shows the comment change.
	saveDiffRendered string

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

	saveBtn := NewButtonRow("Save", "Reset", "Back")
	saveBtn.ZonePrefix = prefix + "save-"

	return &EditScreen{
		sk:           sk,
		fileSelector: NewFileSelector(ModeLoadFile, "Select private key file", sk.Styles()),
		commentIn:    comment,
		saveBtn:      saveBtn,
		zonePrefix:   prefix,
		focus:        editFocusSelectFile,
	}
}

func (s *EditScreen) HasActiveTextInput() bool {
	return s.commentIn.Focused()
}

func (s *EditScreen) HasModal() bool {
	return s.fileSelector.Visible()
}

func (s *EditScreen) Init() tea.Cmd {
	// Don't show file selector here: Init runs at startup when Agent is active.
	// Picker's async messages would be routed to activeTab (Agent), not Edit.
	// We show it on first WindowSizeMsg when Edit becomes active (see Update).
	return nil
}

func (s *EditScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if s.fileSelector.Visible() {
		switch msg.(type) {
		case tea.WindowSizeMsg, FileSelectedMsg, FilePickerCancelledMsg, editKeyLoadedMsg, editSaveMsg, ButtonFlashDoneMsg:
			// Handle these below
		default:
			return s, s.fileSelector.Update(msg)
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.fileSelector.SetHeight(max(s.height-fileSelectorHeightReserve, fileSelectorMinHeight))
		// Show file selector when Edit tab becomes active and no key loaded.
		// Deferred from Init so picker's async messages route to this tab.
		if s.rawKey == nil && !s.fileSelector.Visible() {
			return s, s.fileSelector.Show()
		}
		return s, nil

	case FileSelectedMsg:
		s.status = ""
		s.statusErr = false
		return s, editLoadKeyCmd(msg.Path)

	case FilePickerCancelledMsg:
		// Return focus to tab bar; keep file picker visible (user can press down to re-enter)
		return s, navToTabBarCmd()

	case editKeyLoadedMsg:
		if msg.err != nil {
			contracted := utils.ContractHomeDirectory(msg.filePath)
			s.status = contracted + ": " + msg.err.Error()
			s.statusErr = true
			return s, nil // keep file picker visible
		}
		s.fileSelector.Hide()
		s.keyType = msg.keyType
		s.fingerprint = msg.fingerprint
		s.pubKeyStr = msg.pubKeyStr
		s.rawKey = msg.rawKey
		s.loadedPath = msg.filePath
		s.originalComment = msg.comment
		s.saveDiffRendered = ""
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
			s.saveDiffRendered = ""
			s.originalComment = s.commentIn.Value()
			s.focus = editFocusComment
			s.saveBtn.Focused = false
			s.saveBtn.ClearPress()
			return s, s.commentIn.Focus()
		}
		return s, nil

	case ButtonFlashDoneMsg:
		s.saveBtn.ClearPress()
		return s, nil

	case tea.MouseReleaseMsg:
		if msg.Button != tea.MouseLeft || s.fileSelector.Visible() {
			return s, nil
		}
		return s.handleMouse(msg.X, msg.Y)

	case tea.KeyPressMsg:
		if s.fileSelector.Visible() {
			return s, s.fileSelector.Update(msg)
		}
		if s.focus == editFocusComment && s.commentIn.Focused() {
			return s.handleCommentInput(msg)
		}
		return s.handleKeys(msg)
	}

	return s, nil
}

func (s *EditScreen) handleMouse(x, y int) (tea.Model, tea.Cmd) {
	if s.rawKey == nil && inZoneBounds(s.zonePrefix+"select-file", x, y) {
		s.focus = editFocusSelectFile
		return s, s.fileSelector.Show()
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
			s.saveBtn.Active = btn
			if btn == 1 {
				s.commentIn.SetValue(s.originalComment)
				s.status = "reset to original"
				s.statusErr = false
				s.focus = editFocusComment
				s.saveBtn.Focused = false
				s.saveBtn.ClearPress()
				return s, s.commentIn.Focus()
			}
			if btn == 2 {
				s.editGoBack()
				return s, s.fileSelector.Show()
			}
			comment := strings.TrimSpace(s.commentIn.Value())
			if comment == strings.TrimSpace(s.originalComment) {
				s.status = "no changes"
				s.statusErr = false
				s.focus = editFocusComment
				s.saveBtn.Focused = false
				return s, s.commentIn.Focus()
			}
			if comment == "" {
				s.status = "comment cannot be empty"
				s.statusErr = true
				return s, nil
			}
			s.saveBtn.Press()
			return s, tea.Batch(editSaveKeyCmd(s.rawKey, comment, s.loadedPath), ButtonFlashCmd())
		}
	}
	return s, nil
}

func (s *EditScreen) handleKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		if s.focus == editFocusSave {
			return s, navToTabBarCmd()
		}
		return s, tea.Quit
	case "down", "j":
		return s, s.advanceFocus(1)
	case "up", "k":
		return s, s.advanceFocus(-1)
	case "left", "h", "right", "l":
		if s.rawKey != nil && s.focus == editFocusSave {
			if msg.String() == "left" || msg.String() == "h" {
				s.saveBtn.Left()
			} else {
				s.saveBtn.Right()
			}
			return s, nil
		}
		return s, nil
	case "enter":
		switch s.focus {
		case editFocusSelectFile:
			return s, s.fileSelector.Show()
		case editFocusComment:
			return s, s.commentIn.Focus()
		case editFocusSave:
			if s.rawKey == nil {
				s.status = "no key loaded"
				s.statusErr = true
				return s, nil
			}
			if s.saveBtn.Active == 1 {
				s.commentIn.SetValue(s.originalComment)
				s.status = "reset to original"
				s.statusErr = false
				s.focus = editFocusComment
				s.saveBtn.Focused = false
				s.saveBtn.ClearPress()
				return s, s.commentIn.Focus()
			}
			if s.saveBtn.Active == 2 {
				s.editGoBack()
				return s, s.fileSelector.Show()
			}
			comment := strings.TrimSpace(s.commentIn.Value())
			if comment == strings.TrimSpace(s.originalComment) {
				s.status = "no changes"
				s.statusErr = false
				return s, nil
			}
			if comment == "" {
				s.status = "comment cannot be empty"
				s.statusErr = true
				return s, nil
			}
			s.saveBtn.Press()
			return s, tea.Batch(editSaveKeyCmd(s.rawKey, comment, s.loadedPath), ButtonFlashCmd())
		}
	}
	return s, nil
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

func (s *EditScreen) editGoBack() {
	s.rawKey = nil
	s.loadedPath = ""
	s.originalComment = ""
	s.keyType = ""
	s.fingerprint = ""
	s.pubKeyStr = ""
	s.saveDiffRendered = ""
	s.commentIn.SetValue("")
	s.status = ""
	s.statusErr = false
	s.focus = editFocusSelectFile
	s.saveBtn.Focused = false
	s.saveBtn.ClearPress()
}

func (s *EditScreen) advanceFocus(dir int) tea.Cmd {
	s.commentIn.Blur()
	next := s.focus + dir
	maxFocus := editFocusSelectFile
	if s.rawKey != nil {
		maxFocus = editFocusSave
	}
	if next < editFocusSelectFile {
		return navToTabBarCmd()
	}
	if next > maxFocus {
		next = maxFocus
	}
	s.focus = next
	s.saveBtn.Focused = s.focus == editFocusSave
	if s.focus == editFocusComment {
		return s.commentIn.Focus()
	}
	return nil
}

func (s *EditScreen) View() tea.View {
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

	w := width
	if w < 1 {
		w = defaultViewWidth
	}
	st := s.sk.Styles()
	var sections []string

	if s.rawKey == nil {
		selectStyle := st.AccentStyle
		selectLabel := "  Select key file"
		if active && s.focus == editFocusSelectFile {
			selectStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color(s.sk.Theme().Focus)).Bold(true)
			selectLabel = "> Select key file"
		}
		sections = append(sections, zone.Mark(s.zonePrefix+"select-file", selectStyle.Render(selectLabel)))
	} else {
		sections = append(sections, "")

		infoW := w * 3 / 4
		if infoW > sectionBoxMaxWidth {
			infoW = sectionBoxMaxWidth
		}
		if infoW < sectionBoxMinWidth {
			infoW = sectionBoxMinWidth
		}

		sections = append(sections, st.SectionBox("Public Key",
			st.AccentStyle.Render(truncate(s.pubKeyStr, infoW-6)), infoW, false))

		sections = append(sections, st.SectionBox("Fingerprint",
			st.AccentStyle.Render(s.fingerprint), infoW, false))

		sections = append(sections, zone.Mark(s.zonePrefix+"comment", st.SectionBox("Comment", s.commentIn.View(), infoW, active && s.focus == editFocusComment)))

		// Save + Diff in one full-width box so right edge aligns with boxes above
		s.saveBtn.Focused = active && s.focus == editFocusSave
		savePart := " " + s.saveBtn.View(st)
		comment := strings.TrimSpace(s.commentIn.Value())
		orig := strings.TrimSpace(s.originalComment)
		diffPart := ""
		if comment != orig {
			diffPart = renderCommentDiff(st, orig, comment)
		} else {
			diffPart = st.DimStyle.Render("  (no changes)")
		}
		inner := lipgloss.JoinHorizontal(lipgloss.Top, savePart, "    ", diffPart)
		sections = append(sections, st.SectionBox("Save / Diff", inner, infoW, active && s.focus == editFocusSave))
	}

	if s.rawKey == nil && s.status != "" {
		statusStyle := st.FocusStyle
		if s.statusErr {
			statusStyle = st.ErrorStyle
		}
		sections = append(sections, statusStyle.Render("  "+s.status))
	}

	content := strings.Join(sections, "\n")
	return tea.NewView(lipgloss.Place(w, height, lipgloss.Center, lipgloss.Top,
		lipgloss.NewStyle().Padding(1, 2).Render(content)))
}

func (s *EditScreen) StatusTextRaw() (string, bool) {
	return s.status, s.statusErr
}

// renderCommentDiff returns a styled diff of old vs new comment, side by side.
func renderCommentDiff(st Styles, oldComment, newComment string) string {
	var parts []string
	if oldComment != "" {
		parts = append(parts, st.ErrorStyle.Render("- "+oldComment))
	}
	if newComment != "" {
		parts = append(parts, "    ", st.FocusStyle.Render("+ "+newComment))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
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
				return editKeyLoadedMsg{filePath: path, err: fmt.Errorf("is not an unencrypted OpenSSH key")}
			}
			return editKeyLoadedMsg{filePath: path, err: err}
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
