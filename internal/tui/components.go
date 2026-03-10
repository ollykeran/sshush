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

// NewButtonRow creates a ButtonRow with the given labels.
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

func (b ButtonRow) View(st Styles) string {
	var parts []string
	for i, label := range b.Labels {
		var style lipgloss.Style
		switch {
		case b.Pressed == i:
			style = st.FocusedButtonStyle
		case b.Active == i && b.Focused:
			style = st.FocusedButtonStyle
		case b.Active == i:
			style = st.ButtonActiveStyle
		default:
			style = st.UnfocusedButtonStyle
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

// NewKeyTable creates a KeyTable with type, fingerprint, and comment columns.
func NewKeyTable(width, height int, st Styles) KeyTable {
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
	t.SetStyles(keyTableStyles(rowW, st))
	return KeyTable{Table: t}
}

func (kt *KeyTable) SetSize(width, height int, st Styles) {
	innerW := keyBoxInnerWidth(width)
	rowW := innerW + keyCellPadOverhead
	kt.Table.SetColumns(keyTableColumns(innerW))
	kt.Table.SetWidth(rowW)
	kt.Table.SetStyles(keyTableStyles(rowW, st))
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

func (kt KeyTable) FocusedBoxView(st Styles, focused bool) string {
	border := st.UnfocusedBorderStyle
	if focused {
		border = st.FocusedBorderStyle
	}
	return border.Render(kt.Table.View())
}

func (kt KeyTable) BoxView(st Styles) string {
	return st.UnfocusedBorderStyle.Render(kt.Table.View())
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

func keyTableStyles(rowWidth int, st Styles) table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(st.TableHeaderFgHex)).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color(st.TableHeaderFgHex)).
		Padding(0, 1)
	s.Cell = s.Cell.
		Foreground(lipgloss.Color(st.TableCellFgHex)).
		Padding(0, 1)
	s.Selected = lipgloss.NewStyle().
		Foreground(lipgloss.Color(st.TableSelectedFgHex)).
		Background(lipgloss.Color(st.TableSelectedBgHex)).
		Bold(true).
		Width(rowWidth)
	return s
}

// StyledFilePicker wraps bubbles/filepicker with sshush defaults.
type StyledFilePicker struct {
	Model   filepicker.Model
	Visible bool
}

// NewStyledFilePicker creates a file picker with sshush styles; dirOnly restricts selection to directories.
// Starts in ~/.ssh if it exists, otherwise home.
func NewStyledFilePicker(dirOnly bool, st Styles) StyledFilePicker {
	fp := filepicker.New()
	if home, err := os.UserHomeDir(); err == nil {
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
	fp.Styles.Cursor = lipgloss.NewStyle().Foreground(lipgloss.Color(st.TableHeaderFgHex))
	fp.Styles.Directory = lipgloss.NewStyle().Foreground(lipgloss.Color(st.TableCellFgHex)).Bold(true)
	fp.Styles.File = lipgloss.NewStyle().Foreground(lipgloss.Color(st.TableCellFgHex))
	fp.Styles.Selected = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color(st.TableHeaderFgHex)).
		Bold(true)
	fp.Styles.Symlink = lipgloss.NewStyle().Foreground(lipgloss.Color(st.TableHeaderFgHex))
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

func (s StyledFilePicker) CurrentDirectory() string {
	return s.Model.CurrentDirectory
}
