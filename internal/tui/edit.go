package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/keys"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
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
	editFocusSelectFile = iota
	editFocusLoadAgent
	editFocusAgentTable
	editFocusComment
	editFocusSave
)

// EditScreen is the edit tab for changing key comments.
// A key can be loaded from a private key file or from a key currently loaded in the agent.
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

	agentKeys    KeyTable
	agentKeyList []*sshagent.Key
	showAgent    bool
	socketPath   string
	keyPaths     []string
	fromAgent    bool

	// saveDiffRendered is set after a successful save; shows the comment change.
	saveDiffRendered string

	focus     int
	width     int
	height    int
	status    string
	statusErr bool
}

// NewEditScreen creates an EditScreen with the given skeleton, agent socket path,
// and config key paths (used to resolve a key's source file for agent-loaded keys).
func NewEditScreen(sk *Skeleton, socketPath string, keyPaths []string) *EditScreen {
	prefix := zone.NewPrefix()

	comment := textinput.New()
	comment.Prompt = ""
	comment.Placeholder = "key comment"

	saveBtn := NewButtonRow("Save", "Reset", "Back")
	saveBtn.ZonePrefix = prefix + "save-"

	kt := NewKeyTable(defaultViewWidth, defaultExportAgentKeysRows, sk.Styles())
	kt.ZonePrefix = prefix + "agent-"

	return &EditScreen{
		sk:           sk,
		fileSelector: NewFileSelector(ModeLoadFile, "Select private key file", sk.Styles()),
		commentIn:    comment,
		saveBtn:      saveBtn,
		agentKeys:    kt,
		socketPath:   socketPath,
		keyPaths:     keyPaths,
		zonePrefix:   prefix,
		focus:        editFocusSelectFile,
	}
}

func (s *EditScreen) HasActiveTextInput() bool {
	return s.commentIn.Focused()
}

func (s *EditScreen) HasModal() bool {
	return s.fileSelector.Visible() || s.showAgent
}

// hasKey reports whether a key is loaded for editing. Agent-loaded keys may not
// have resolvable source file material (rawKey), but always have a fingerprint.
func (s *EditScreen) hasKey() bool {
	return s.rawKey != nil || s.fingerprint != ""
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
		case tea.WindowSizeMsg, FileSelectedMsg, FilePickerCancelledMsg, editKeyLoadedMsg, editSaveMsg, editAgentKeysMsg, ButtonFlashDoneMsg:
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
		agentKeysRows := s.height / agentKeysTableHeightDiv
		if agentKeysRows < agentKeysTableMinRows {
			agentKeysRows = agentKeysTableMinRows
		}
		if agentKeysRows > agentKeysTableMaxRows {
			agentKeysRows = agentKeysTableMaxRows
		}
		s.agentKeys.SetSize(s.width, agentKeysRows, s.sk.Styles())
		// Show file selector when Edit tab becomes active and no key loaded.
		// Deferred from Init so picker's async messages route to this tab.
		if !s.hasKey() && !s.fileSelector.Visible() {
			return s, s.fileSelector.Show()
		}
		return s, nil

	case FileSelectedMsg:
		s.status = ""
		s.statusErr = false
		s.fromAgent = false
		return s, editLoadKeyCmd(msg.Path)

	case FilePickerCancelledMsg:
		// Return focus to tab bar; keep file picker visible (user can press down to re-enter)
		return s, navToTabBarCmd()

	case editAgentKeysMsg:
		if msg.err != nil {
			s.status = msg.err.Error()
			s.statusErr = true
			return s, nil
		}
		if len(msg.keys) == 0 {
			s.status = "no keys loaded in agent"
			s.statusErr = true
			return s, nil
		}
		s.agentKeyList = msg.keys
		rows := make([]table.Row, len(msg.keys))
		for i, k := range msg.keys {
			rows[i] = table.Row{k.Type(), ssh.FingerprintSHA256(k), k.Comment}
		}
		s.agentKeys.SetRows(rows)
		s.showAgent = true
		s.focus = editFocusAgentTable
		return s, nil

	case editKeyLoadedMsg:
		if msg.err != nil {
			if msg.filePath != "" {
				s.status = utils.DisplayPath(msg.filePath) + ": " + msg.err.Error()
			} else {
				s.status = msg.err.Error()
			}
			s.statusErr = true
			return s, nil
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
		if s.fromAgent {
			if msg.filePath != "" {
				s.status = "loaded from agent: " + utils.DisplayPath(msg.filePath)
			} else {
				s.status = "loaded from agent"
			}
		} else {
			s.status = "loaded: " + utils.DisplayPath(msg.filePath)
		}
		s.statusErr = false
		s.focus = editFocusComment
		return s, s.commentIn.Focus()

	case editSaveMsg:
		if msg.err != nil {
			s.status = "save failed: " + msg.err.Error()
			s.statusErr = true
		} else {
			if s.fromAgent {
				if s.loadedPath != "" {
					s.status = "saved: " + utils.DisplayPath(s.loadedPath)
				} else {
					s.status = "saved"
				}
			} else {
				s.status = "saved: " + utils.DisplayPath(s.loadedPath)
			}
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
		if msg.Button != tea.MouseLeft || s.fileSelector.Visible() || s.showAgent {
			return s, nil
		}
		return s.handleMouse(msg.X, msg.Y)

	case tea.KeyPressMsg:
		if s.fileSelector.Visible() {
			return s, s.fileSelector.Update(msg)
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
	if !s.hasKey() {
		if inZoneBounds(s.zonePrefix+"select-file", x, y) {
			s.focus = editFocusSelectFile
			s.fromAgent = false
			return s, s.fileSelector.Show()
		}
		if inZoneBounds(s.zonePrefix+"load-agent", x, y) {
			s.focus = editFocusLoadAgent
			return s, editFetchAgentKeysCmd(s.socketPath)
		}
	}
	if s.hasKey() {
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
			return s.saveComment()
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
		if s.hasKey() && s.focus == editFocusSave {
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
		case editFocusLoadAgent:
			return s, editFetchAgentKeysCmd(s.socketPath)
		case editFocusComment:
			return s, s.commentIn.Focus()
		case editFocusSave:
			if !s.hasKey() {
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
			return s.saveComment()
		}
	}
	return s, nil
}

// saveComment validates the current comment and issues the save command
// (file-based or agent-loaded, which syncs disk, agent, and vault config).
func (s *EditScreen) saveComment() (tea.Model, tea.Cmd) {
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
	var saveCmd tea.Cmd
	if s.fromAgent {
		saveCmd = editSaveAgentKeyCmd(s.socketPath, s.keyPaths, s.fingerprint, comment)
	} else {
		saveCmd = editSaveKeyCmd(s.rawKey, comment, s.loadedPath)
	}
	return s, tea.Batch(saveCmd, ButtonFlashCmd())
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

func (s *EditScreen) handleAgentTable(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.showAgent = false
		s.focus = editFocusLoadAgent
		return s, nil
	case "enter":
		row := s.agentKeys.SelectedRow()
		if row == nil {
			s.status = "no key selected"
			s.statusErr = true
			return s, nil
		}
		idx := s.agentKeys.Table.Cursor()
		if idx < 0 || idx >= len(s.agentKeyList) {
			s.status = "key selection unavailable"
			s.statusErr = true
			return s, nil
		}
		s.showAgent = false
		s.fromAgent = true
		s.status = ""
		s.statusErr = false
		return s, editLoadAgentKeyCmd(s.keyPaths, s.agentKeyList[idx])
	}
	cmd := s.agentKeys.Update(msg)
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
	s.fromAgent = false
	s.showAgent = false
	s.agentKeyList = nil
	s.status = ""
	s.statusErr = false
	s.focus = editFocusSelectFile
	s.saveBtn.Focused = false
	s.saveBtn.ClearPress()
}

func (s *EditScreen) advanceFocus(dir int) tea.Cmd {
	s.commentIn.Blur()
	next := s.focus + dir
	maxFocus := editFocusLoadAgent
	if s.hasKey() {
		maxFocus = editFocusSave
	}
	if next < editFocusSelectFile {
		return navToTabBarCmd()
	}
	if next > maxFocus {
		next = maxFocus
	}
	// Skip agent table focus when not showing
	if next == editFocusAgentTable && !s.showAgent {
		next += dir
		if next < editFocusSelectFile {
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

	if s.showAgent {
		st := s.sk.Styles()
		title := st.SectionTitleStyle.Render("Select key from agent")
		return tea.NewView(lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			title+"\n"+s.agentKeys.FocusedBoxView(st, true)))
	}

	w := width
	if w < 1 {
		w = defaultViewWidth
	}
	st := s.sk.Styles()
	var sections []string

	if !s.hasKey() {
		fileFocused := active && s.focus == editFocusSelectFile
		agentFocused := active && s.focus == editFocusLoadAgent
		fileStyle := st.AccentStyle
		agentStyle := st.AccentStyle
		fileLabel := "  Load from file"
		agentLabel := "  Load from agent"
		if fileFocused {
			fileStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color(s.sk.Theme().Focus)).Bold(true)
			fileLabel = "> Load from file"
		}
		if agentFocused {
			agentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color(s.sk.Theme().Focus)).Bold(true)
			agentLabel = "> Load from agent"
		}
		sections = append(sections,
			zone.Mark(s.zonePrefix+"select-file", fileStyle.Render(fileLabel)),
			zone.Mark(s.zonePrefix+"load-agent", agentStyle.Render(agentLabel)),
		)
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

	if s.status != "" {
		statusStyle := st.FocusStyle
		if s.statusErr {
			statusStyle = st.ErrorStyle
		}
		sections = append(sections, "", statusStyle.Render("  "+s.status))
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

// editFetchAgentKeysCmd lists the keys currently loaded in the running agent.
// For the vault backend these are the identities the vault exposes to the agent.
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

// editLoadAgentKeyCmd loads a key selected from the running agent into the edit
// form. The comment and public key come from the agent; when the source file is
// resolvable (config key_paths or the fingerprint registry) the raw key material
// is loaded so the file can be rewritten on save. When it is not, the key is still
// editable for the vault backend, which persists comments in the vault config.
func editLoadAgentKeyCmd(keyPaths []string, k *sshagent.Key) tea.Cmd {
	return func() tea.Msg {
		pub, err := ssh.ParsePublicKey(k.Blob)
		if err != nil {
			return editKeyLoadedMsg{err: fmt.Errorf("parse agent key: %w", err)}
		}
		fp := ssh.FingerprintSHA256(pub)
		comment := strings.TrimSpace(k.Comment)
		path := agent.ResolveFilepath(fp, keyPaths)
		fileExists := false
		if path != "" {
			if _, err := os.Stat(path); err == nil {
				fileExists = true
			}
		}

		var rawKey interface{}
		keyType := strings.TrimPrefix(pub.Type(), "ssh-")
		if fileExists {
			parsed, raw, _, loadErr := keys.LoadKeyMaterial(path)
			if loadErr != nil {
				// Source file unreadable (e.g. encrypted); keep the agent metadata
				// so vault-mode edits still work. Save will surface a clear error
				// for the keys backend.
				if comment == "" {
					comment = filepath.Base(path)
				}
			} else {
				rawKey = raw
				keyType = parsed.KeyType
				if comment == "" {
					comment = parsed.Comment
				}
			}
		}

		pubLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
		if comment != "" {
			pubLine += " " + comment
		}

		return editKeyLoadedMsg{
			keyType:     keyType,
			comment:     comment,
			fingerprint: fp,
			pubKeyStr:   pubLine,
			rawKey:      rawKey,
			filePath:    path,
		}
	}
}

// editSaveAgentKeyCmd saves a comment edit for a key loaded from the agent.
// For the vault backend it persists the comment in the vault config via the
// vault-set-comment extension. When the source file is known it rewrites the
// key file on disk, then reloads the key in the running agent so the agent
// reports the new comment.
func editSaveAgentKeyCmd(socketPath string, keyPaths []string, fingerprint, comment string) tea.Cmd {
	return func() tea.Msg {
		comment = strings.TrimSpace(comment)
		if comment == "" {
			return editSaveMsg{err: fmt.Errorf("comment cannot be empty")}
		}

		mode, live := agent.LiveBackendMode(socketPath)
		if !live {
			return editSaveMsg{err: fmt.Errorf("agent not running")}
		}

		path := agent.ResolveFilepath(fingerprint, keyPaths)
		fileExists := false
		if path != "" {
			if _, err := os.Stat(path); err == nil {
				fileExists = true
			}
		}

		// Vault backend: the config stores per-key comments, so persist the change there.
		if mode == "vault" {
			payload := vault.BuildSetCommentPayload(fingerprint, comment)
			if _, err := agent.CallExtension(socketPath, vault.ExtensionVaultSetComment, payload); err != nil {
				return editSaveMsg{err: fmt.Errorf("vault: %w", err)}
			}
		}

		// Update the key file on disk when its source path is known.
		if fileExists {
			_, _, rawKey, err := agent.ParseKeyFromPath(path)
			if err != nil {
				return editSaveMsg{err: fmt.Errorf("load key file: %w", err)}
			}
			if err := keys.SaveWithComment(rawKey, comment, path); err != nil {
				return editSaveMsg{err: fmt.Errorf("save key file: %w", err)}
			}
			agent.RegisterFilepath(fingerprint, path)
		} else if mode != "vault" {
			if path != "" {
				return editSaveMsg{err: fmt.Errorf("source key file not found: %s", utils.DisplayPath(path))}
			}
			return editSaveMsg{err: fmt.Errorf("source file path unknown; add the key via config key_paths")}
		}

		// Reload the key in the running agent so its reported comment changes.
		if mode != "vault" && fileExists {
			if _, err := agent.RemoveKeyFromSocketByFingerprint(socketPath, fingerprint); err != nil {
				return editSaveMsg{err: fmt.Errorf("remove old key: %w", err)}
			}
			if err := agent.AddKeyToSocketFromPath(socketPath, path); err != nil {
				return editSaveMsg{err: fmt.Errorf("reload key: %w", err)}
			}
		}

		return editSaveMsg{}
	}
}
