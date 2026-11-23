package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/keys"
	"github.com/ollykeran/sshush/internal/utils"
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
	createFocusPath
	createFocusSave
)

var (
	keyTypes     = []string{"ed25519", "rsa", "ecdsa"}
	rsaOptions   = []string{"2048", "3072", "4096"}
	ecdsaOptions = []string{"256", "384", "521"}
)

// CreateScreen is the create tab for generating new SSH keypairs.
type CreateScreen struct {
	sk        *Skeleton
	typeRow   ButtonRow
	optionRow ButtonRow
	commentIn textinput.Model
	pathIn    textinput.Model // where the private key goes; the public key gets .pub added
	saveBtn   ButtonRow

	zonePrefix  string
	lastKeyType string
	confirm     confirmDialog // asks before Save overwrites an existing key file
	ring        FocusRing
	width       int
	height      int

	genResult *keyGenDoneMsg
	status    string
	statusErr bool
}

// NewCreateScreen creates a CreateScreen with default type, comment, and output path.
func NewCreateScreen(sk *Skeleton) *CreateScreen {
	prefix := zone.NewPrefix()

	comment := textinput.New()
	comment.Prompt = ""
	comment.SetValue(keys.DefaultComment())

	pathIn := textinput.New()
	pathIn.Prompt = ""
	pathIn.SetValue(defaultKeyPath("ed25519"))

	typeRow := NewButtonRow(keyTypes...)
	typeRow.ZonePrefix = prefix + "type-"
	optionRow := NewButtonRow(rsaOptions...)
	optionRow.ZonePrefix = prefix + "opt-"
	saveBtn := NewButtonRow("Save")
	saveBtn.ZonePrefix = prefix + "save-"

	s := &CreateScreen{
		sk:          sk,
		typeRow:     typeRow,
		optionRow:   optionRow,
		commentIn:   comment,
		pathIn:      pathIn,
		saveBtn:     saveBtn,
		zonePrefix:  prefix,
		lastKeyType: "ed25519",
		confirm:     newConfirmDialog(),
		ring:        NewFocusRing(5), // type, options, comment, path, save
	}
	s.ring.SetSkip(func(i int) bool {
		return i == createFocusOptions && s.currentKeyType() == "ed25519"
	})
	return s
}

func (s *CreateScreen) HasModal() bool { return s.confirm.active }

// ModalTakesEsc lets esc reach the overwrite popup, where it answers no.
func (s *CreateScreen) ModalTakesEsc() bool { return s.confirm.active }

func (s *CreateScreen) HasActiveTextInput() bool {
	return s.commentIn.Focused() || s.pathIn.Focused()
}

// defaultKeyFile is the file name ssh-keygen would use for a key type.
func defaultKeyFile(keyType string) string { return "id_" + keyType }

// defaultKeyPath is where a new key of keyType goes by default, as the Path
// field shows it: ~/.ssh/id_<type>.
func defaultKeyPath(keyType string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".ssh", defaultKeyFile(keyType))
	}
	return utils.ContractHomeDirectory(filepath.Join(home, ".ssh", defaultKeyFile(keyType)))
}

func (s *CreateScreen) Init() tea.Cmd {
	return nil
}

func (s *CreateScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		return s, nil

	case keyGenDoneMsg:
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

	case tea.MouseReleaseMsg:
		if msg.Button != tea.MouseLeft {
			return s, nil
		}
		if s.confirm.active {
			return s.answerOverwrite(s.confirm.HandleMouse(msg.X, msg.Y))
		}
		return s.handleMouse(msg.X, msg.Y)

	case tea.KeyPressMsg:
		if s.confirm.active {
			return s.answerOverwrite(s.confirm.Update(msg))
		}
		if s.ring.Index() == createFocusComment && s.commentIn.Focused() {
			return s.handleCommentInput(msg)
		}
		if s.ring.Index() == createFocusPath && s.pathIn.Focused() {
			return s.handlePathInput(msg)
		}
		return s.handleKeys(msg)
	}
	return s, nil
}

func (s *CreateScreen) handleMouse(x, y int) (tea.Model, tea.Cmd) {
	if btn := s.typeRow.HandleMouse(x, y); btn >= 0 {
		s.blurInputs()
		s.ring.SetIndex(createFocusType)
		s.typeRow.Active = btn
		s.syncKeyTypeChange()
		s.updateButtonFocus()
		return s, nil
	}
	if s.currentKeyType() != "ed25519" {
		if btn := s.optionRow.HandleMouse(x, y); btn >= 0 {
			s.blurInputs()
			s.ring.SetIndex(createFocusOptions)
			s.optionRow.Active = btn
			s.updateButtonFocus()
			return s, nil
		}
	}
	if inZoneBounds(s.zonePrefix+"comment", x, y) {
		s.blurInputs()
		s.ring.SetIndex(createFocusComment)
		s.updateButtonFocus()
		cmd := s.commentIn.Focus()
		if pos := sectionBoxCursorPos(s.zonePrefix+"comment", x, y); pos >= 0 {
			s.commentIn.SetCursor(pos)
		}
		return s, cmd
	}
	if inZoneBounds(s.zonePrefix+"path", x, y) {
		s.blurInputs()
		s.ring.SetIndex(createFocusPath)
		s.updateButtonFocus()
		cmd := s.pathIn.Focus()
		if pos := sectionBoxCursorPos(s.zonePrefix+"path", x, y); pos >= 0 {
			s.pathIn.SetCursor(pos)
		}
		return s, cmd
	}
	if btn := s.saveBtn.HandleMouse(x, y); btn >= 0 {
		s.blurInputs()
		s.ring.SetIndex(createFocusSave)
		s.updateButtonFocus()
		return s.doSave()
	}
	return s, nil
}

func (s *CreateScreen) handleKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	focus := s.ring.Index()

	switch {
	case KeyMatches(msg, KeyQuit):
		return s, tea.Quit

	case KeyMatches(msg, KeyDown):
		return s.focusNext()
	case KeyMatches(msg, KeyUp):
		return s.focusPrev()

	case KeyMatches(msg, KeyLeft):
		switch focus {
		case createFocusType:
			s.typeRow.Left()
			s.syncKeyTypeChange()
		case createFocusOptions:
			s.optionRow.Left()
		}
		return s, nil

	case KeyMatches(msg, KeyRight):
		switch focus {
		case createFocusType:
			s.typeRow.Right()
			s.syncKeyTypeChange()
		case createFocusOptions:
			s.optionRow.Right()
		}
		return s, nil

	case KeyMatches(msg, KeyEnter):
		switch focus {
		case createFocusComment:
			return s, s.commentIn.Focus()
		case createFocusPath:
			return s, s.pathIn.Focus()
		case createFocusSave:
			return s.doSave()
		}
		return s, nil
	}
	return s, nil
}

func (s *CreateScreen) handleCommentInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

func (s *CreateScreen) handlePathInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.pathIn.Blur()
		return s, nil
	case "tab", "down":
		s.pathIn.Blur()
		return s.focusNext()
	case "shift+tab", "up":
		s.pathIn.Blur()
		return s.focusPrev()
	}
	var cmd tea.Cmd
	s.pathIn, cmd = s.pathIn.Update(msg)
	return s, cmd
}

func (s *CreateScreen) focusNext() (tea.Model, tea.Cmd) {
	s.blurInputs()
	s.ring.Next()
	s.updateButtonFocus()
	return s, s.focusInput()
}

func (s *CreateScreen) focusPrev() (tea.Model, tea.Cmd) {
	s.blurInputs()
	if s.ring.Prev() {
		return s, navToTabBarCmd()
	}
	s.updateButtonFocus()
	return s, s.focusInput()
}

func (s *CreateScreen) focusInput() tea.Cmd {
	switch s.ring.Index() {
	case createFocusComment:
		return s.commentIn.Focus()
	case createFocusPath:
		return s.pathIn.Focus()
	}
	return nil
}

func (s *CreateScreen) blurInputs() {
	s.commentIn.Blur()
	s.pathIn.Blur()
}

func (s *CreateScreen) updateButtonFocus() {
	focus := s.ring.Index()
	s.typeRow.Focused = focus == createFocusType
	s.optionRow.Focused = focus == createFocusOptions
	s.saveBtn.Focused = focus == createFocusSave
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

func (s *CreateScreen) syncKeyTypeChange() {
	kt := s.currentKeyType()
	if kt == s.lastKeyType {
		return
	}
	s.renameDefaultKeyFile(s.lastKeyType, kt)
	s.lastKeyType = kt
	switch kt {
	case "rsa":
		s.optionRow = NewButtonRow(rsaOptions...)
		s.optionRow.ZonePrefix = s.zonePrefix + "opt-"
	case "ecdsa":
		s.optionRow = NewButtonRow(ecdsaOptions...)
		s.optionRow.ZonePrefix = s.zonePrefix + "opt-"
	}
}

// renameDefaultKeyFile follows a key type change in the Path field: when it
// still ends in the default file name for the old type (id_ed25519), that is
// swapped for the new type's (id_rsa), keeping the directory. A file name the
// user typed is left alone.
func (s *CreateScreen) renameDefaultKeyFile(oldType, newType string) {
	dir, file := splitKeyPath(s.pathIn.Value())
	if file != defaultKeyFile(oldType) {
		return
	}
	s.pathIn.SetValue(dir + defaultKeyFile(newType))
}

// splitKeyPath splits a Path field value after its last separator, keeping the
// value's own spelling (~, either slash on Windows) for the directory part.
func splitKeyPath(value string) (dir, file string) {
	i := strings.LastIndexAny(value, "/"+string(filepath.Separator))
	return value[:i+1], value[i+1:]
}

// savePath is the directory and file name Save writes the private key to (the
// public key goes beside it with .pub added), and the two joined. It reads the
// Path field with ~ expanded. A Path that is empty, ends in a separator or
// names an existing directory gets the key type's default file name
// (id_ed25519) added.
func (s *CreateScreen) savePath() (dir, filename, fullPath string) {
	value := strings.TrimSpace(s.pathIn.Value())
	if value == "" {
		value = defaultKeyPath(s.currentKeyType())
	}
	full := utils.ExpandHomeDirectory(value)
	_, file := splitKeyPath(value)
	if info, err := os.Stat(full); file == "" || (err == nil && info.IsDir()) {
		full = filepath.Join(full, defaultKeyFile(s.currentKeyType()))
	}
	full = filepath.Clean(full)
	return filepath.Dir(full), filepath.Base(full), full
}

// doSave generates and saves the key, first asking whether to overwrite when
// the private key file already exists.
func (s *CreateScreen) doSave() (tea.Model, tea.Cmd) {
	_, _, fullPath := s.savePath()
	if _, err := os.Stat(fullPath); err == nil {
		s.confirm.Show("Overwrite key?",
			utils.DisplayPath(fullPath)+"\n"+
				"already exists. Saving replaces it and its .pub file.")
		return s, nil
	}
	return s.generate()
}

// answerOverwrite acts on the overwrite question once it has an answer.
func (s *CreateScreen) answerOverwrite(result confirmResult) (tea.Model, tea.Cmd) {
	switch result {
	case confirmYes:
		return s.generate()
	case confirmNo:
		s.status = "not saved"
		s.statusErr = false
	}
	return s, nil
}

func (s *CreateScreen) generate() (tea.Model, tea.Cmd) {
	dir, filename, _ := s.savePath()

	s.saveBtn.Press()
	s.genResult = nil

	keyType := s.currentKeyType()
	bits := s.currentBits()
	comment := s.commentIn.Value()
	if comment == "" {
		comment = keys.DefaultComment()
	}

	return s, tea.Batch(generateKeyCmd(keyType, bits, comment, dir, filename), ButtonFlashCmd())
}

func (s *CreateScreen) View() tea.View {
	width := s.width
	height := s.height
	if width < 1 {
		width = defaultViewWidth
	}
	if height < 1 {
		height = defaultViewHeight
	}
	active := s.sk.ScreenActive()
	w := width
	if w < 1 {
		w = defaultViewWidth
	}

	if s.confirm.active {
		return tea.NewView(lipgloss.Place(w, height, lipgloss.Center, lipgloss.Center,
			s.confirm.View(s.sk.Styles(), min(SectionWidth(w), 72))))
	}

	leftW := w / 2
	if leftW < minCreatePanelWidth {
		leftW = w - 4
	}
	rightW := w - leftW - 4

	left := s.viewCreatePanel(leftW, active)
	right := s.viewResultPanel(rightW)

	if w >= minWidthForHorizontalLayout {
		content := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
		return tea.NewView(lipgloss.Place(w, height, lipgloss.Center, lipgloss.Top, content))
	}
	content := left + "\n" + right
	return tea.NewView(lipgloss.Place(w, height, lipgloss.Center, lipgloss.Top, content))
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

	st := s.sk.Styles()
	focused := func(region int) bool {
		return active && s.ring.Index() == region
	}

	sections = append(sections, st.SectionBox("Type", s.typeRow.View(st), w, focused(createFocusType)))

	kt := s.currentKeyType()
	if kt == "rsa" || kt == "ecdsa" {
		sections = append(sections, st.SectionBox("Options", s.optionRow.View(st), w, focused(createFocusOptions)))
	}

	sections = append(sections, zone.Mark(s.zonePrefix+"comment", st.SectionBox("Comment", s.commentIn.View(), w, focused(createFocusComment))))

	sections = append(sections, zone.Mark(s.zonePrefix+"path", st.SectionBox("Path", s.pathIn.View(), w, focused(createFocusPath))))

	sections = append(sections, " "+s.saveBtn.View(st))

	// Below Save, set apart by a blank line: the warning that Save will ask to
	// overwrite, then the last save's status.
	_, _, fullPath := s.savePath()
	if _, err := os.Stat(fullPath); err == nil {
		sections = append(sections, "", st.WarnStyle.Render("  ⚠ File exists: "+utils.DisplayPath(fullPath)))
	}

	if s.status != "" {
		style := st.FocusStyle
		if s.statusErr {
			style = st.ErrorStyle
		}
		sections = append(sections, style.Render("  "+s.status))
	}

	return strings.Join(sections, "\n")
}

// viewResultPanel shows the Public Key, Private Key and Public Key File boxes.
// They are drawn before a key is generated too, so the page does not shift when
// one is: until then the file boxes preview, dimmed, where Save will write.
func (s *CreateScreen) viewResultPanel(w int) string {
	st := s.sk.Styles()
	var pubKey, privPath, pubPath string
	if s.genResult != nil {
		pubKey = st.AccentStyle.Render(s.genResult.pubKeyStr)
		privPath = st.AccentStyle.Render(utils.DisplayPath(s.genResult.privPath))
		pubPath = st.AccentStyle.Render(utils.DisplayPath(s.genResult.pubPath))
	} else {
		_, _, full := s.savePath()
		pubKey = st.DimStyle.Render("shown here after Save")
		privPath = st.DimStyle.Render(utils.DisplayPath(full))
		pubPath = st.DimStyle.Render(utils.DisplayPath(full + ".pub"))
	}

	return strings.Join([]string{
		st.SectionBox("Public Key", pubKey, w, false),
		st.SectionBox("Private Key", privPath, w, false),
		st.SectionBox("Public Key File", pubPath, w, false),
	}, "\n")
}

func (s *CreateScreen) StatusTextRaw() (string, bool) {
	return s.status, s.statusErr
}

func generateKeyCmd(keyType string, bits int, comment, dir, filename string) tea.Cmd {
	return func() tea.Msg {
		privPEM, pubAuth, err := keys.Generate(keyType, bits, comment)
		if err != nil {
			return keyGenDoneMsg{err: err}
		}
		privPath := filepath.Join(dir, filename)
		pubPath := privPath + ".pub"

		if err := keys.SavePair(dir, filename, privPEM, pubAuth); err != nil {
			return keyGenDoneMsg{err: fmt.Errorf("save keypair: %w", err)}
		}

		pubKeyStr := strings.TrimSpace(string(pubAuth))

		return keyGenDoneMsg{
			pubKeyStr: pubKeyStr,
			privPath:  privPath,
			pubPath:   pubPath,
		}
	}
}
