package tui

import (
	"os"
	"strings"

	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone"
)

// ButtonRow is a horizontal row of navigable buttons with consistent styling.
type ButtonRow struct {
	Labels     []string
	Active     int
	Pressed    int
	Focused    bool
	ZonePrefix string
}

func NewButtonRow(labels ...string) ButtonRow {
	return ButtonRow{
		Labels:  labels,
		Pressed: -1,
	}
}

func (b *ButtonRow) Left() {
	b.Active = (b.Active - 1 + len(b.Labels)) % len(b.Labels)
}

func (b *ButtonRow) Right() {
	b.Active = (b.Active + 1) % len(b.Labels)
}

func (b *ButtonRow) Press() {
	b.Pressed = b.Active
}

func (b *ButtonRow) ClearPress() {
	b.Pressed = -1
}

func (b ButtonRow) View() string {
	var parts []string
	for i, label := range b.Labels {
		var style lipgloss.Style
		switch {
		case b.Pressed == i:
			style = FocusedButtonStyle
		case b.Active == i && b.Focused:
			style = FocusedButtonStyle
		case b.Active == i:
			style = lipgloss.NewStyle().
				Foreground(ColorBright).
				Background(ColorPurple).
				Bold(true).
				Padding(0, 2)
		default:
			style = UnfocusedButtonStyle
		}
		rendered := style.Render(label)
		if b.ZonePrefix != "" {
			rendered = zone.Mark(b.ZonePrefix+label, rendered)
		}
		parts = append(parts, rendered)
	}
	return strings.Join(parts, " ")
}

func (b ButtonRow) HandleMouse(x, y int) int {
	if b.ZonePrefix == "" {
		return -1
	}
	for i, label := range b.Labels {
		if inZoneBounds(b.ZonePrefix+label, x, y) {
			return i
		}
	}
	return -1
}

// KeyTable wraps bubbles/table with sshush styling.
type KeyTable struct {
	Table      table.Model
	ZonePrefix string
}

const keyCellPadOverhead = 6

func NewKeyTable(width, height int) KeyTable {
	innerW := keyBoxInnerWidth(width)
	rowW := innerW + keyCellPadOverhead
	cols := keyTableColumns(innerW)
	t := table.New(
		table.WithColumns(cols),
		table.WithRows([]table.Row{}),
		table.WithFocused(true),
		table.WithHeight(height),
		table.WithWidth(rowW),
	)
	t.SetStyles(keyTableStyles(rowW))
	return KeyTable{Table: t}
}

func (kt *KeyTable) SetSize(width, height int) {
	innerW := keyBoxInnerWidth(width)
	rowW := innerW + keyCellPadOverhead
	kt.Table.SetColumns(keyTableColumns(innerW))
	kt.Table.SetWidth(rowW)
	kt.Table.SetStyles(keyTableStyles(rowW))
	kt.Table.SetHeight(height)
}

func (kt *KeyTable) SetRows(rows []table.Row) {
	kt.Table.SetRows(rows)
}

func (kt *KeyTable) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	kt.Table, cmd = kt.Table.Update(msg)
	return cmd
}

func (kt KeyTable) View() string {
	view := kt.Table.View()
	if kt.ZonePrefix != "" {
		view = zone.Mark(kt.ZonePrefix+"table", view)
	}
	return view
}

func (kt KeyTable) HandleMouse(x, y int) int {
	if kt.ZonePrefix == "" {
		return -1
	}
	z := zone.Get(kt.ZonePrefix + "table")
	if z == nil {
		return -1
	}
	if x < z.StartX || x > z.EndX || y < z.StartY || y > z.EndY {
		return -1
	}
	row := y - z.StartY - 2
	rows := kt.Table.Rows()
	if row < 0 || row >= len(rows) {
		return -1
	}
	return row
}

func (kt KeyTable) SelectedRow() table.Row {
	return kt.Table.SelectedRow()
}

func (kt KeyTable) FocusedBoxView(focused bool) string {
	border := UnfocusedBorderStyle
	if focused {
		border = FocusedBorderStyle
	}
	return border.Render(kt.Table.View())
}

func (kt KeyTable) BoxView() string {
	return UnfocusedBorderStyle.Render(kt.Table.View())
}

func keyBoxInnerWidth(termWidth int) int {
	boxW := termWidth * 3 / 4
	if boxW > 120 {
		boxW = 120
	}
	if boxW < 60 {
		boxW = 60
	}
	return boxW - 4
}

func keyTableColumns(w int) []table.Column {
	if w < 36 {
		w = 36
	}
	typeW := 19
	fpW := 51
	commentW := w - typeW - fpW
	if commentW < 20 {
		commentW = 20
		fpW = w - typeW - commentW
		if fpW < 30 {
			fpW = 30
		}
	}
	return []table.Column{
		{Title: "Type", Width: typeW},
		{Title: "Fingerprint", Width: fpW},
		{Title: "Comment", Width: commentW},
	}
}

func keyTableStyles(rowWidth int) table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(ColorDim).
		BorderBottom(true).
		Bold(true).
		Foreground(ColorGreen).
		Padding(0, 1)
	s.Cell = s.Cell.
		Foreground(ColorPink).
		Padding(0, 1)
	s.Selected = lipgloss.NewStyle().
		Foreground(ColorBright).
		Background(ColorPurple).
		Bold(true).
		Width(rowWidth)
	return s
}

// StyledFilePicker wraps bubbles/filepicker with sshush defaults.
type StyledFilePicker struct {
	Model   filepicker.Model
	Visible bool
}

func NewStyledFilePicker(dirOnly bool) StyledFilePicker {
	fp := filepicker.New()
	home, err := os.UserHomeDir()
	if err == nil {
		sshDir := home + "/.ssh"
		if info, statErr := os.Stat(sshDir); statErr == nil && info.IsDir() {
			fp.CurrentDirectory = sshDir
		} else {
			fp.CurrentDirectory = home
		}
	}
	fp.DirAllowed = dirOnly
	fp.FileAllowed = !dirOnly
	fp.ShowHidden = true
	fp.Styles.Cursor = lipgloss.NewStyle().Foreground(ColorGreen)
	fp.Styles.Directory = lipgloss.NewStyle().Foreground(ColorPink).Bold(true)
	fp.Styles.File = lipgloss.NewStyle().Foreground(ColorPink)
	fp.Styles.Selected = lipgloss.NewStyle().Foreground(ColorBright).Bold(true)
	fp.Styles.Symlink = lipgloss.NewStyle().Foreground(ColorDim)
	return StyledFilePicker{Model: fp}
}

func (s *StyledFilePicker) SetHeight(h int) {
	s.Model.SetHeight(h)
}

func (s *StyledFilePicker) Init() tea.Cmd {
	return s.Model.Init()
}

func (s *StyledFilePicker) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	s.Model, cmd = s.Model.Update(msg)
	return cmd
}

func (s StyledFilePicker) View() string {
	return s.Model.View()
}

func (s StyledFilePicker) DidSelectFile(msg tea.Msg) (bool, string) {
	return s.Model.DidSelectFile(msg)
}
