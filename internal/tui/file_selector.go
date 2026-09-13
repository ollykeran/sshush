package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/utils"
)

// FileSelectedMsg is sent when the user selects a file or directory.
type FileSelectedMsg struct {
	Path string
}

// FilePickerCancelledMsg is sent when the user cancels (e.g. esc).
type FilePickerCancelledMsg struct{}

// FileSelectorMode controls whether the picker allows files or directories only.
type FileSelectorMode int

const (
	ModeLoadFile FileSelectorMode = iota
	ModeDirectory
)

// FileSelector wraps StyledFilePicker with modal, show/hide state, and typed messages.
type FileSelector struct {
	picker     StyledFilePicker
	visible    bool
	title      string
	mode       FileSelectorMode
	zonePrefix string
}

// NewFileSelector creates a FileSelector with the given mode and title.
func NewFileSelector(mode FileSelectorMode, title string, st Styles) *FileSelector {
	dirOnly := mode == ModeDirectory
	return &FileSelector{
		picker:     NewStyledFilePicker(dirOnly, st),
		title:      title,
		mode:       mode,
		zonePrefix: zone.NewPrefix(),
	}
}

// Show makes the selector visible and returns the Init cmd for the picker.
func (f *FileSelector) Show() tea.Cmd {
	f.visible = true
	return f.picker.Init()
}

// Hide makes the selector not visible.
func (f *FileSelector) Hide() {
	f.visible = false
}

// Visible returns whether the selector is currently shown.
func (f *FileSelector) Visible() bool {
	return f.visible
}

// SetHeight sets the picker height.
func (f *FileSelector) SetHeight(h int) {
	f.picker.SetHeight(h)
}

// Init returns the picker Init cmd (used when already visible).
func (f *FileSelector) Init() tea.Cmd {
	return f.picker.Init()
}

// Update handles messages; returns cmd that may send FileSelectedMsg or FilePickerCancelledMsg.
func (f *FileSelector) Update(msg tea.Msg) tea.Cmd {
	if !f.visible {
		return nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q":
			return func() tea.Msg { return FilePickerCancelledMsg{} }
		}
		cmd := f.picker.Update(msg)
		if didSelect, path := f.picker.DidSelectFile(msg); didSelect {
			return tea.Batch(cmd, func() tea.Msg { return FileSelectedMsg{Path: path} })
		}
		return cmd
	case tea.MouseReleaseMsg:
		if msg.Button != tea.MouseLeft {
			return nil
		}
		return f.handleMouse(msg.X, msg.Y)
	default:
		return f.picker.Update(msg)
	}
}

// handleMouse translates a click inside the file picker into the equivalent
// key presses. Returns nil if the click doesn't land on anything actionable.
func (f *FileSelector) handleMouse(x, y int) tea.Cmd {
	if inZoneBounds(f.zonePrefix+"updir", x, y) {
		return f.pressKeyBinding("backspace")
	}

	if !inZoneBounds(f.zonePrefix+"box", x, y) {
		return func() tea.Msg { return FilePickerCancelledMsg{} }
	}

	for i := 0; i <= f.picker.Model.Height(); i++ {
		id := f.zonePrefix + "row-" + strconv.Itoa(i)
		if !inZoneBounds(id, x, y) {
			continue
		}
		return f.selectRow(i)
	}

	return nil
}

// pressKeyBinding sends a synthetic key press by name (e.g. "backspace",
// "down", "up") straight to the underlying picker.
func (f *FileSelector) pressKeyBinding(name string) tea.Cmd {
	var msg tea.KeyPressMsg
	switch name {
	case "backspace":
		msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		msg = tea.KeyPressMsg{Code: tea.KeyUp}
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	default:
		return nil
	}
	return f.picker.Update(msg)
}

// selectRow moves the picker's selection to visual row i (0-based, matching
// the current render) and opens/selects it, as if the user had arrowed to
// that row and pressed enter.
func (f *FileSelector) selectRow(i int) tea.Cmd {
	lines := strings.Split(f.picker.View(), "\n")
	if i >= len(lines) {
		return nil
	}
	clicked := ansi.Strip(lines[i])
	if strings.TrimSpace(clicked) == "" {
		return nil
	}

	cursor := f.picker.Model.Cursor
	currentRow := -1
	for row, line := range lines {
		stripped := ansi.Strip(line)
		if strings.HasPrefix(stripped, cursor) {
			currentRow = row
			break
		}
	}
	if currentRow < 0 {
		return nil
	}

	var cmds []tea.Cmd
	delta := i - currentRow
	step := "down"
	if delta < 0 {
		step = "up"
		delta = -delta
	}
	for n := 0; n < delta; n++ {
		if cmd := f.pressKeyBinding(step); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	enterMsg := tea.KeyPressMsg{Code: tea.KeyEnter}
	cmds = append(cmds, f.picker.Update(enterMsg))
	if didSelect, path := f.picker.DidSelectFile(enterMsg); didSelect {
		cmds = append(cmds, func() tea.Msg { return FileSelectedMsg{Path: path} })
	}
	return tea.Batch(cmds...)
}

// View returns the modal content when visible, or empty string when hidden.
// Parent should use lipgloss.Place to center it.
// focused controls border style: pink when false, green when true.
func (f *FileSelector) View(width, height int, focused bool, st Styles) string {
	if !f.visible {
		return ""
	}
	// Use width - 2 to fit inside skeleton's side borders; 4 cols padding each side
	usableW := width - 2
	if usableW < fileSelectorMinUsableWidth {
		usableW = fileSelectorMinUsableWidth
	}
	pad := 4
	boxW := usableW - 2*pad
	innerW := boxW - 6 // border + padding
	if innerW < fileSelectorMinInnerWidth {
		innerW = fileSelectorMinInnerWidth
	}

	title := st.SectionTitleStyle.Render(f.title)
	//	hint := st.DimStyle.Render("→/l in ←/h out")
	dirPath := f.picker.CurrentDirectory()
	dirPart := st.BannerStyle.Render(utils.DisplayPath(dirPath))
	lineW := usableW - 2*pad
	hintLine := zone.Mark(f.zonePrefix+"updir", lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(lineW-lineW/2).Align(lipgloss.Left).Render(dirPart)))

	pickerView := f.picker.View()
	var truncated []string
	for i, line := range strings.Split(pickerView, "\n") {
		line = ansi.Truncate(line, innerW, "...")
		truncated = append(truncated, zone.Mark(f.zonePrefix+"row-"+strconv.Itoa(i), line))
	}

	border := st.UnfocusedBorderStyle
	if focused {
		border = st.FocusedBorderStyle
	}
	boxContent := zone.Mark(f.zonePrefix+"box", border.Width(boxW).Render(strings.Join(truncated, "\n")))
	block := lipgloss.JoinVertical(lipgloss.Left, title, "", hintLine, boxContent)
	return lipgloss.NewStyle().Padding(0, pad).PaddingTop(1).PaddingBottom(1).Render(block)
}
