package tui

import (
	"charm.land/lipgloss/v2"
	"github.com/ollykeran/sshush/internal/theme"
)

const tabWidth = 10

// Styles holds all lipgloss styles derived from a theme. Built by BuildStyles.
type Styles struct {
	TitleStyle                lipgloss.Style
	ActiveTabStyle            lipgloss.Style
	ActiveTabFocusedStyle     lipgloss.Style
	InactiveTabStyle          lipgloss.Style
	FocusedBorderStyle        lipgloss.Style
	UnfocusedBorderStyle      lipgloss.Style
	SectionTitleStyle         lipgloss.Style
	FocusedButtonStyle        lipgloss.Style
	UnfocusedButtonStyle      lipgloss.Style
	ButtonActiveStyle         lipgloss.Style // active but not focused; same padding as FocusedButtonStyle to avoid resize
	ErrorStyle                lipgloss.Style
	DimStyle                  lipgloss.Style
	AccentStyle               lipgloss.Style
	FocusStyle                lipgloss.Style
	WarnStyle                 lipgloss.Style
	HeaderTabActive           lipgloss.Style
	HeaderTabActiveUnfocused  lipgloss.Style
	HeaderTabActiveFocused    lipgloss.Style
	HeaderTabInactive         lipgloss.Style
	HeaderTabBoxActiveFocused lipgloss.Style
	HeaderTabBoxActive        lipgloss.Style
	HeaderTabBoxInactive      lipgloss.Style
	DaemonLabelStyle          lipgloss.Style
	DaemonBoxUnfocused        lipgloss.Style
	DaemonBoxFocused          lipgloss.Style
	BannerStyle               lipgloss.Style
	// Hex strings for borders and table (use lipgloss.Color(st.XXX) at use site)
	OuterBorderColorHex string
	TableHeaderFgHex    string
	TableCellFgHex      string
	TableSelectedFgHex  string
	TableSelectedBgHex  string
}

func headerTabBorder() lipgloss.Border {
	b := lipgloss.RoundedBorder()
	b.Right = "├"
	b.Left = "┤"
	return b
}

// BuildStyles returns a Styles struct built from the given theme.
func BuildStyles(t theme.Theme) Styles {
	text := lipgloss.Color(t.Text)
	focus := lipgloss.Color(t.Focus)
	accent := lipgloss.Color(t.Accent)
	errClr := lipgloss.Color(t.Error)
	warnClr := lipgloss.Color(t.Warning)
	black := lipgloss.Color("#000000")

	return Styles{
		TitleStyle: lipgloss.NewStyle().Bold(true).Foreground(focus),
		ActiveTabStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(black).
			Background(accent).
			Padding(0, 1).
			Width(tabWidth).
			Align(lipgloss.Center),
		ActiveTabFocusedStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(black).
			Background(focus).
			Padding(0, 1).
			Width(tabWidth).
			Align(lipgloss.Center),
		InactiveTabStyle: lipgloss.NewStyle().
			Foreground(accent).
			Padding(0, 1).
			Width(tabWidth).
			Align(lipgloss.Center),
		FocusedBorderStyle: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(focus).
			Bold(true).
			Padding(0, 1),
		UnfocusedBorderStyle: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(0, 1),
		SectionTitleStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(focus),
		FocusedButtonStyle: lipgloss.NewStyle().
			Background(focus).
			Foreground(black).
			Bold(true).
			Padding(0, 2),
		UnfocusedButtonStyle: lipgloss.NewStyle().
			Foreground(accent).
			Padding(0, 2),
		ButtonActiveStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(black).
			Background(accent).
			Padding(0, 2),
		ErrorStyle:          lipgloss.NewStyle().Foreground(errClr),
		DimStyle:            lipgloss.NewStyle().Foreground(text),
		AccentStyle:         lipgloss.NewStyle().Foreground(accent),
		FocusStyle:          lipgloss.NewStyle().Bold(true).Foreground(focus),
		WarnStyle:           lipgloss.NewStyle().Foreground(warnClr),
		OuterBorderColorHex: t.Accent,
		HeaderTabActive: lipgloss.NewStyle().
			Bold(true).
			Foreground(black).
			Background(accent).
			PaddingLeft(2).PaddingRight(2).
			BorderStyle(headerTabBorder()).
			BorderForeground(accent),
		HeaderTabActiveUnfocused: lipgloss.NewStyle().
			Bold(true).
			Foreground(accent).
			PaddingLeft(2).PaddingRight(2).
			BorderStyle(headerTabBorder()).
			BorderForeground(accent),
		HeaderTabActiveFocused: lipgloss.NewStyle().
			Bold(true).
			Foreground(black).
			Background(focus).
			PaddingLeft(2).PaddingRight(2).
			BorderStyle(headerTabBorder()).
			BorderForeground(focus),
		HeaderTabInactive: lipgloss.NewStyle().
			Foreground(accent).
			PaddingLeft(2).PaddingRight(2).
			BorderStyle(headerTabBorder()).
			BorderForeground(accent),
		HeaderTabBoxActiveFocused: lipgloss.NewStyle().
			Bold(true).
			Foreground(black).
			Background(focus).
			PaddingLeft(2).PaddingRight(2).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(focus),
		HeaderTabBoxActive: lipgloss.NewStyle().
			Bold(true).
			Foreground(black).
			Background(accent).
			PaddingLeft(2).PaddingRight(2).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(accent),
		HeaderTabBoxInactive: lipgloss.NewStyle().
			Foreground(accent).
			PaddingLeft(2).PaddingRight(2).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(accent),
		DaemonLabelStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(accent).
			PaddingRight(1),
		DaemonBoxUnfocused: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			PaddingLeft(1).PaddingRight(1),
		DaemonBoxFocused: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(focus).
			PaddingLeft(1).PaddingRight(1),
		BannerStyle: lipgloss.NewStyle().
			Foreground(accent).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(0, 2),
		TableHeaderFgHex:   t.Focus,
		TableCellFgHex:     t.Text,
		TableSelectedFgHex: "#000000",
		TableSelectedBgHex: t.Focus,
	}
}

// SectionBox renders a titled box with optional focus styling for TUI sections. a titled box with optional focus styling for TUI sections.
func (st Styles) SectionBox(title, content string, width int, focused bool) string {
	t := st.SectionTitleStyle.Render(title)
	innerW := width - 4
	if innerW < 10 {
		innerW = 10
	}
	border := st.UnfocusedBorderStyle
	if focused {
		border = st.FocusedBorderStyle
	}
	box := border.Width(innerW).Render(content)
	return t + "\n" + box
}

// HelpRow formats a help line with key and description for the help overlay.
func (st Styles) HelpRow(key, desc string) string {
	k := st.FocusStyle.Width(14).Render(key)
	d := st.AccentStyle.Render(desc)
	return "  " + k + d
}
