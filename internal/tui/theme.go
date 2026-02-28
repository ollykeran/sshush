package tui

import (
	"fmt"
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

	tabWidth = 10

	ActiveTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorBright).
			Background(ColorPurple).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(ColorPurple).
			Width(tabWidth).
			Align(lipgloss.Center)

	ActiveTabFocusedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorBlack).
				Background(ColorGreen).
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(ColorGreen).
				Width(tabWidth).
				Align(lipgloss.Center)

	InactiveTabStyle = lipgloss.NewStyle().
				Foreground(ColorPink).
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(ColorPink).
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
)

const Banner = "              ██                   ██    \n██▀▀▀▀ ██▀▀▀▀ ██▀▀██ ██  ██ ██▀▀▀▀ ██▀▀██\n▀▀▀▀██ ▀▀▀▀██ ██  ██ ██  ██ ▀▀▀▀██ ██  ██\n▀▀▀▀▀▀ ▀▀▀▀▀▀ ▀▀  ▀▀ ▀▀▀▀▀▀ ▀▀▀▀▀▀ ▀▀  ▀▀"

var BannerStyle = lipgloss.NewStyle().
	Foreground(ColorPink).
	BorderStyle(lipgloss.RoundedBorder()).
	BorderForeground(ColorPink).
	Padding(0, 2)

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

func RenderTabBar(tabs []string, activeIdx, width int, tabBarFocused bool, rightContent string) string {
	var parts []string
	for i, tab := range tabs {
		marked := zone.Mark("tab-"+tab, tab)
		var rendered string
		switch {
		case i == activeIdx && tabBarFocused:
			rendered = ActiveTabFocusedStyle.Render(marked)
		case i == activeIdx:
			rendered = ActiveTabStyle.Render(marked)
		default:
			rendered = InactiveTabStyle.Render(marked)
		}
		parts = append(parts, rendered)
	}
	left := lipgloss.JoinHorizontal(lipgloss.Center, parts...)
	bar := left
	if rightContent != "" {
		leftW := lipgloss.Width(left)
		rightW := lipgloss.Width(rightContent)
		leftH := lipgloss.Height(left)
		gap := width - leftW - rightW
		if gap < 1 {
			gap = 1
		}
		spacer := lipgloss.NewStyle().Width(gap).Height(leftH).Render("")
		bar = lipgloss.JoinHorizontal(lipgloss.Center, left, spacer, rightContent)
	}
	rule := DimStyle.Render(strings.Repeat("─", width))
	return bar + "\n" + rule
}

func HelpHint(width, height int) string {
	size := DimStyle.Render(fmt.Sprintf("%dx%d", width, height))
	hint := DimStyle.Render("? help")
	gap := width - lipgloss.Width(size) - lipgloss.Width(hint)
	if gap < 1 {
		gap = 1
	}
	return size + lipgloss.NewStyle().Width(gap).Render("") + hint
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
