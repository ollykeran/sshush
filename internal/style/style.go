package style

import "github.com/charmbracelet/lipgloss"

var (
	green  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7EE787"))
	pink   = lipgloss.NewStyle().Foreground(lipgloss.Color("#F472B6"))
	purple = lipgloss.NewStyle().Foreground(lipgloss.Color("#631596"))
	err    = lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171"))
	box    = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#631596")).
		Padding(0, 1)
)

func Green(s string) string  { return green.Render(s) }
func Pink(s string) string   { return pink.Render(s) }
func Purple(s string) string { return purple.Render(s) }
func Err(s string) string    { return err.Render(s) }
func Box(s string) string    { return box.Render(s) }
