package tui

import "charm.land/lipgloss/v2"

var (
	ColorGreen  = lipgloss.Color("#7EE787")
	ColorPink   = lipgloss.Color("#F472B6")
	ColorPurple = lipgloss.Color("#631596")
	ColorErr    = lipgloss.Color("#F87171")
	ColorDim    = lipgloss.Color("#585858") // 240
	ColorBright = lipgloss.Color("#ffffaf") // 229
	ColorBlack  = lipgloss.Color("#000000")
)

var (
	TitleStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorGreen)

	tabWidth = 10

	ActiveTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorBright).
			Background(ColorPurple).
			Padding(0, 1).
			Width(tabWidth).
			Align(lipgloss.Center)

	ActiveTabFocusedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorBlack).
				Background(ColorGreen).
				Padding(0, 1).
				Width(tabWidth).
				Align(lipgloss.Center)

	InactiveTabStyle = lipgloss.NewStyle().
				Foreground(ColorPink).
				Padding(0, 1).
				Width(tabWidth).
				Align(lipgloss.Center)

	FocusedBorderStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(ColorGreen).
				Bold(true).
				Padding(0, 1)

	UnfocusedBorderStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(ColorPurple).
				Padding(0, 1)

	SectionBorderStyle = UnfocusedBorderStyle

	SectionTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorGreen)

	FocusedButtonStyle = lipgloss.NewStyle().
				Background(ColorGreen).
				Foreground(ColorBlack).
				Bold(true).
				Padding(0, 2)

	UnfocusedButtonStyle = lipgloss.NewStyle().
				Foreground(ColorPink).
				Padding(0, 2)

	ErrorStyle = lipgloss.NewStyle().Foreground(ColorErr)
	DimStyle   = lipgloss.NewStyle().Foreground(ColorDim)
	PinkStyle  = lipgloss.NewStyle().Foreground(ColorPink)
	GreenStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorGreen)
	WarnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#F2E94E"))

	OuterBorderColor = ColorPurple
)

func headerTabBorder() lipgloss.Border {
	b := lipgloss.RoundedBorder()
	b.Right = "├"
	b.Left = "┤"
	return b
}

var (
	HeaderTabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorBright).
			Background(ColorPurple).
			PaddingLeft(2).PaddingRight(2).
			BorderStyle(headerTabBorder()).
			BorderForeground(ColorPurple)

	// HeaderTabActiveUnfocused: same border as inactive, but bold; no purple background (avoids lingering purple box)
	HeaderTabActiveUnfocused = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorPink).
				PaddingLeft(2).PaddingRight(2).
				BorderStyle(headerTabBorder()).
				BorderForeground(ColorPurple)

	HeaderTabActiveFocused = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorBlack).
				Background(ColorGreen).
				PaddingLeft(2).PaddingRight(2).
				BorderStyle(headerTabBorder()).
				BorderForeground(ColorGreen)

	HeaderTabInactive = lipgloss.NewStyle().
				Foreground(ColorPink).
				PaddingLeft(2).PaddingRight(2).
				BorderStyle(headerTabBorder()).
				BorderForeground(ColorPurple)
)

const Banner = "              ██                   ██    \n██▀▀▀▀ ██▀▀▀▀ ██▀▀██ ██  ██ ██▀▀▀▀ ██▀▀██\n▀▀▀▀██ ▀▀▀▀██ ██  ██ ██  ██ ▀▀▀▀██ ██  ██\n▀▀▀▀▀▀ ▀▀▀▀▀▀ ▀▀  ▀▀ ▀▀▀▀▀▀ ▀▀▀▀▀▀ ▀▀  ▀▀"

var BannerStyle = lipgloss.NewStyle().
	Foreground(ColorPink).
	BorderStyle(lipgloss.RoundedBorder()).
	BorderForeground(ColorPink).
	Padding(0, 2)

// SectionBox renders a titled box with optional focus styling for TUI sections.
func SectionBox(title, content string, width int, focused bool) string {
	t := SectionTitleStyle.Render(title)
	innerW := width - 4
	if innerW < 10 {
		innerW = 10
	}
	border := UnfocusedBorderStyle
	if focused {
		border = FocusedBorderStyle
	}
	box := border.Width(innerW).Render(content)
	return t + "\n" + box
}

// HelpRow formats a help line with key and description for the help overlay.
func HelpRow(key, desc string) string {
	k := lipgloss.NewStyle().Foreground(ColorGreen).Bold(true).Width(14).Render(key)
	d := PinkStyle.Render(desc)
	return "  " + k + d
}
