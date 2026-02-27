package tui

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"golang.org/x/crypto/ssh"
)

type keyGenDoneMsg struct {
	pubKeyStr string
	privPath  string
	pubPath   string
	err       error
}

const (
	createFocusType = iota
	createFocusOptions
	createFocusComment
	createFocusDir
	createFocusFilename
	createFocusSave
)

var (
	keyTypes     = []string{"ed25519", "rsa", "ecdsa"}
	rsaOptions   = []string{"2048", "3072", "4096"}
	ecdsaOptions = []string{"256", "384", "521"}
)

type CreateScreen struct {
	typeRow    ButtonRow
	optionRow  ButtonRow
	commentIn  textinput.Model
	dirInput   textinput.Model
	filenameIn textinput.Model
	saveBtn    ButtonRow

	lastKeyType    string
	fileEdited     bool
	confirmSave    bool
	focus          int
	width          int
	height         int

	genResult *keyGenDoneMsg
	status    string
	statusErr bool
}

func NewCreateScreen() *CreateScreen {
	comment := textinput.New()
	comment.Prompt = ""
	comment.SetValue(defaultComment())

	filename := textinput.New()
	filename.Prompt = ""
	filename.SetValue("id_ed25519")

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".ssh")

	dirIn := textinput.New()
	dirIn.Prompt = ""
	dirIn.SetValue(dir)

	return &CreateScreen{
		typeRow:     NewButtonRow(keyTypes...),
		optionRow:   NewButtonRow(rsaOptions...),
		commentIn:   comment,
		dirInput:    dirIn,
		filenameIn:  filename,
		saveBtn:     NewButtonRow("Save"),
		lastKeyType: "ed25519",
		focus:       createFocusType,
	}
}

func (s *CreateScreen) Init() tea.Cmd {
	return nil
}

func (s *CreateScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		return s, nil

	case keyGenDoneMsg:
		s.confirmSave = false
		if msg.err != nil {
			s.status = msg.err.Error()
			s.statusErr = true
		} else {
			s.genResult = &msg
			s.status = "key saved"
			s.statusErr = false
		}
		return s, nil

	case ButtonFlashDoneMsg:
		s.saveBtn.ClearPress()
		return s, nil

	case tea.KeyPressMsg:
		if s.focus == createFocusComment && s.commentIn.Focused() {
			return s.handleCommentInput(msg)
		}
		if s.focus == createFocusDir && s.dirInput.Focused() {
			return s.handleDirInput(msg)
		}
		if s.focus == createFocusFilename && s.filenameIn.Focused() {
			return s.handleFilenameInput(msg)
		}
		return s.handleKeys(msg)
	}
	return s, nil
}

func (s *CreateScreen) handleKeys(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	if !(msg.String() == "enter" && s.focus == createFocusSave) {
		s.confirmSave = false
	}

	switch msg.String() {
	case "q", "esc":
		return s, tea.Quit

	case "down", "j":
		return s.focusNext()
	case "up", "k":
		return s.focusPrev()

	case "left", "h":
		switch s.focus {
		case createFocusType:
			s.typeRow.Left()
			s.syncKeyTypeChange()
		case createFocusOptions:
			s.optionRow.Left()
		}
		return s, nil

	case "right", "l":
		switch s.focus {
		case createFocusType:
			s.typeRow.Right()
			s.syncKeyTypeChange()
		case createFocusOptions:
			s.optionRow.Right()
		}
		return s, nil

	case "enter":
		switch s.focus {
		case createFocusComment:
			return s, s.commentIn.Focus()
		case createFocusDir:
			return s, s.dirInput.Focus()
		case createFocusFilename:
			return s, s.filenameIn.Focus()
		case createFocusSave:
			return s.doSave()
		}
		return s, nil
	}
	return s, nil
}

func (s *CreateScreen) handleCommentInput(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.commentIn.Blur()
		return s, nil
	case "tab", "down":
		s.commentIn.Blur()
		return s.focusNext()
	case "shift+tab", "up":
		s.commentIn.Blur()
		return s.focusPrev()
	}
	var cmd tea.Cmd
	s.commentIn, cmd = s.commentIn.Update(msg)
	return s, cmd
}

func (s *CreateScreen) handleDirInput(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.dirInput.Blur()
		return s, nil
	case "tab", "down":
		s.dirInput.Blur()
		return s.focusNext()
	case "shift+tab", "up":
		s.dirInput.Blur()
		return s.focusPrev()
	}
	var cmd tea.Cmd
	s.dirInput, cmd = s.dirInput.Update(msg)
	return s, cmd
}

func (s *CreateScreen) handleFilenameInput(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.filenameIn.Blur()
		return s, nil
	case "tab", "down":
		s.filenameIn.Blur()
		return s.focusNext()
	case "shift+tab", "up":
		s.filenameIn.Blur()
		return s.focusPrev()
	}
	s.fileEdited = true
	var cmd tea.Cmd
	s.filenameIn, cmd = s.filenameIn.Update(msg)
	return s, cmd
}

func (s *CreateScreen) focusNext() (Screen, tea.Cmd) {
	s.blurInputs()
	if s.currentKeyType() == "ed25519" && s.focus == createFocusType {
		s.focus = createFocusComment
		return s, s.focusInput()
	}
	if s.focus < createFocusSave {
		s.focus++
	} else {
		s.focus = createFocusType
	}
	s.updateButtonFocus()
	return s, s.focusInput()
}

func (s *CreateScreen) focusPrev() (Screen, tea.Cmd) {
	s.blurInputs()
	if s.currentKeyType() == "ed25519" && s.focus == createFocusComment {
		s.focus = createFocusType
		s.updateButtonFocus()
		return s, nil
	}
	if s.focus > createFocusType {
		s.focus--
		s.updateButtonFocus()
		return s, s.focusInput()
	}
	return s, navToTabBarCmd()
}

func (s *CreateScreen) focusInput() tea.Cmd {
	switch s.focus {
	case createFocusComment:
		return s.commentIn.Focus()
	case createFocusDir:
		return s.dirInput.Focus()
	case createFocusFilename:
		return s.filenameIn.Focus()
	}
	return nil
}

func (s *CreateScreen) blurInputs() {
	s.commentIn.Blur()
	s.dirInput.Blur()
	s.filenameIn.Blur()
}

func (s *CreateScreen) updateButtonFocus() {
	s.typeRow.Focused = s.focus == createFocusType
	s.optionRow.Focused = s.focus == createFocusOptions
	s.saveBtn.Focused = s.focus == createFocusSave
}

func (s *CreateScreen) currentKeyType() string {
	return keyTypes[s.typeRow.Active]
}

func (s *CreateScreen) currentBits() int {
	switch s.currentKeyType() {
	case "rsa":
		opts := rsaOptions
		v := opts[s.optionRow.Active%len(opts)]
		var bits int
		fmt.Sscanf(v, "%d", &bits)
		return bits
	case "ecdsa":
		opts := ecdsaOptions
		v := opts[s.optionRow.Active%len(opts)]
		var bits int
		fmt.Sscanf(v, "%d", &bits)
		return bits
	}
	return 0
}

func (s *CreateScreen) updateFilename() {
	s.filenameIn.SetValue("id_" + s.currentKeyType())
}

func (s *CreateScreen) syncKeyTypeChange() {
	kt := s.currentKeyType()
	if kt == s.lastKeyType {
		return
	}
	s.lastKeyType = kt
	if !s.fileEdited {
		s.updateFilename()
	}
	switch kt {
	case "rsa":
		s.optionRow = NewButtonRow(rsaOptions...)
	case "ecdsa":
		s.optionRow = NewButtonRow(ecdsaOptions...)
	}
}

func (s *CreateScreen) doSave() (Screen, tea.Cmd) {
	dir := s.dirInput.Value()
	filename := s.filenameIn.Value()
	if filename == "" {
		filename = "id_" + s.currentKeyType()
	}
	fullPath := filepath.Join(dir, filename)

	if !s.confirmSave {
		if _, err := os.Stat(fullPath); err == nil {
			s.confirmSave = true
			s.status = "file exists! press Save again to overwrite"
			s.statusErr = true
			return s, nil
		}
	}
	s.confirmSave = false

	s.saveBtn.Press()
	s.genResult = nil

	keyType := s.currentKeyType()
	bits := s.currentBits()
	comment := s.commentIn.Value()
	if comment == "" {
		comment = defaultComment()
	}

	return s, tea.Batch(generateKeyCmd(keyType, bits, comment, dir, filename), ButtonFlashCmd())
}

func (s *CreateScreen) View(width, height int, active bool) string {
	w := width
	if w < 1 {
		w = 80
	}

	leftW := w / 2
	if leftW < 40 {
		leftW = w - 4
	}
	rightW := w - leftW - 4

	left := s.viewCreatePanel(leftW, active)
	right := s.viewResultPanel(rightW)

	if w >= 100 {
		content := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
		return lipgloss.Place(w, height, lipgloss.Center, lipgloss.Top, content)
	}
	content := left + "\n" + right
	return lipgloss.Place(w, height, lipgloss.Center, lipgloss.Top, content)
}

func (s *CreateScreen) viewCreatePanel(w int, active bool) string {
	var sections []string

	if active {
		s.updateButtonFocus()
	} else {
		s.typeRow.Focused = false
		s.optionRow.Focused = false
		s.saveBtn.Focused = false
	}

	focused := func(region int) bool {
		return active && s.focus == region
	}

	sections = append(sections, SectionBox("Type", s.typeRow.View(), w, focused(createFocusType)))

	kt := s.currentKeyType()
	if kt == "rsa" || kt == "ecdsa" {
		sections = append(sections, SectionBox("Options", s.optionRow.View(), w, focused(createFocusOptions)))
	}

	sections = append(sections, SectionBox("Comment", s.commentIn.View(), w, focused(createFocusComment)))

	sections = append(sections, SectionBox("Directory", s.dirInput.View(), w, focused(createFocusDir)))

	sections = append(sections, SectionBox("Filename", s.filenameIn.View(), w, focused(createFocusFilename)))

	fullPath := filepath.Join(s.dirInput.Value(), s.filenameIn.Value())
	if _, err := os.Stat(fullPath); err == nil {
		sections = append(sections, WarnStyle.Render("  ⚠ File exists: "+fullPath))
	}

	sections = append(sections, " "+s.saveBtn.View())

	if s.status != "" {
		style := GreenStyle
		if s.statusErr {
			style = ErrorStyle
		}
		sections = append(sections, style.Render("  "+s.status))
	}

	return strings.Join(sections, "\n")
}

func (s *CreateScreen) viewResultPanel(w int) string {
	if s.genResult == nil {
		return DimStyle.Render("  Generate a key to see results")
	}

	var sections []string

	sections = append(sections, SectionBox("Public Key", PinkStyle.Render(s.genResult.pubKeyStr), w, false))
	sections = append(sections, SectionBox("Private Key", PinkStyle.Render(s.genResult.privPath), w, false))
	sections = append(sections, SectionBox("Public Key File", PinkStyle.Render(s.genResult.pubPath), w, false))

	return strings.Join(sections, "\n")
}

func (s *CreateScreen) HelpEntries() []string {
	return []string{
		HelpRow("up/k", "Previous field"),
		HelpRow("down/j", "Next field"),
		HelpRow("left/h", "Previous option"),
		HelpRow("right/l", "Next option"),
		HelpRow("enter", "Activate/Edit"),
		"",
		HelpRow("q/esc", "Quit/Cancel"),
	}
}

func defaultComment() string {
	u, err := user.Current()
	name := "user"
	if err == nil {
		name = u.Username
	}
	host, err := os.Hostname()
	if err != nil {
		host = "localhost"
	}
	return name + "@" + host
}

func generateKeyCmd(keyType string, bits int, comment, dir, filename string) tea.Cmd {
	return func() tea.Msg {
		privPEM, pubAuth, err := GenerateKey(keyType, bits, comment)
		if err != nil {
			return keyGenDoneMsg{err: err}
		}
		privPath := filepath.Join(dir, filename)
		pubPath := privPath + ".pub"

		if err := os.MkdirAll(dir, 0700); err != nil {
			return keyGenDoneMsg{err: fmt.Errorf("mkdir: %w", err)}
		}
		if err := os.WriteFile(privPath, privPEM, 0600); err != nil {
			return keyGenDoneMsg{err: fmt.Errorf("write private key: %w", err)}
		}
		if err := os.WriteFile(pubPath, pubAuth, 0644); err != nil {
			return keyGenDoneMsg{err: fmt.Errorf("write public key: %w", err)}
		}

		pubKey, _, err := parsePublicKeyFromPEM(privPEM)
		pubKeyStr := string(pubAuth)
		if err == nil {
			pubKeyStr = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pubKey))) + " " + comment
		}

		return keyGenDoneMsg{
			pubKeyStr: pubKeyStr,
			privPath:  privPath,
			pubPath:   pubPath,
		}
	}
}

func parsePublicKeyFromPEM(privPEM []byte) (ssh.PublicKey, interface{}, error) {
	key, err := ssh.ParseRawPrivateKey(privPEM)
	if err != nil {
		return nil, nil, err
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, nil, err
	}
	return signer.PublicKey(), key, nil
}
