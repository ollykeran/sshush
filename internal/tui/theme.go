package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone"
)

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

	ActiveTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorBright).
			Background(ColorPurple).
			Padding(0, 2)

	ActiveTabFocusedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorBlack).
				Background(ColorGreen).
				Padding(0, 2)

	InactiveTabStyle = lipgloss.NewStyle().
				Foreground(ColorPink).
				Padding(0, 2)

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
)

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

func RenderTabBar(tabs []string, activeIdx, width int, tabBarFocused bool) string {
	var parts []string
	for i, tab := range tabs {
		var rendered string
		switch {
		case i == activeIdx && tabBarFocused:
			rendered = ActiveTabFocusedStyle.Render(tab)
		case i == activeIdx:
			rendered = ActiveTabStyle.Render(tab)
		default:
			rendered = InactiveTabStyle.Render(tab)
		}
		parts = append(parts, zone.Mark("tab-"+tab, rendered))
	}
	bar := " " + lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	rule := DimStyle.Render(strings.Repeat("─", width))
	return bar + "\n" + rule
}

func HelpHint(width int) string {
	hint := DimStyle.Render("? help")
	return lipgloss.NewStyle().Width(width).Align(lipgloss.Right).Render(hint)
}

func HelpRow(key, desc string) string {
	k := lipgloss.NewStyle().Foreground(ColorGreen).Bold(true).Width(14).Render(key)
	d := PinkStyle.Render(desc)
	return "  " + k + d
}

func HelpOverlay(lines []string, width, height int) string {
	body := strings.Join(lines, "\n")
	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorPurple).
		Padding(1, 2).
		Render(body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
