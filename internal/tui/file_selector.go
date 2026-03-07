package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
	picker  StyledFilePicker
	visible bool
	title   string
	mode    FileSelectorMode
}

// NewFileSelector creates a FileSelector with the given mode and title.
func NewFileSelector(mode FileSelectorMode, title string) *FileSelector {
	dirOnly := mode == ModeDirectory
	return &FileSelector{
		picker: NewStyledFilePicker(dirOnly),
		title:  title,
		mode:   mode,
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
	}
	return f.picker.Update(msg)
}

// View returns the modal content when visible, or empty string when hidden.
// Parent should use lipgloss.Place to center it.
// focused controls border style: pink when false, green when true.
func (f *FileSelector) View(width, height int, focused bool) string {
	if !f.visible {
		return ""
	}
	// Use width - 2 to fit inside skeleton's side borders; 4 cols padding each side
	usableW := width - 2
	if usableW < 60 {
		usableW = 60
	}
	pad := 4
	boxW := usableW - 2*pad
	innerW := boxW - 6 // border + padding
	if innerW < 40 {
		innerW = 40
	}

	title := SectionTitleStyle.Render(f.title)
	hint := DimStyle.Render("bksp: up dir | enter: select | q/esc: exit")
	dirPath := f.picker.CurrentDirectory()
	dirPart := PinkStyle.Render("dir: " + dirPath)
	lineW := usableW - 2*pad
	hintLine := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(lineW/2).Align(lipgloss.Left).Render(hint),
		lipgloss.NewStyle().Width(lineW-lineW/2).Align(lipgloss.Right).Render(dirPart))

	pickerView := f.picker.View()
	var truncated []string
	for _, line := range strings.Split(pickerView, "\n") {
		line = ansi.Truncate(line, innerW, "...")
		truncated = append(truncated, line)
	}

	border := UnfocusedBorderStyle
	if focused {
		border = FocusedBorderStyle
	}
	boxContent := border.Width(boxW).Render(strings.Join(truncated, "\n"))
	block := lipgloss.JoinVertical(lipgloss.Left, title, "", hintLine, boxContent)
	return lipgloss.NewStyle().Padding(0, pad).PaddingTop(1).PaddingBottom(1).Render(block)
}
