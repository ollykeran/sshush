package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

type ButtonFlashDoneMsg struct{}

func ButtonFlashCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg {
		return ButtonFlashDoneMsg{}
	})
}

type NavToTabBarMsg struct{}

func navToTabBarCmd() tea.Cmd {
	return func() tea.Msg {
		return NavToTabBarMsg{}
	}
}

func NewTUI(configPath, socketPath string) *Skeleton {
	s := NewSkeleton()
	s.KeyMap.SwitchTabLeft = []string{"left", "h"}
	s.KeyMap.SwitchTabRight = []string{"right", "l"}

	s.AddPage("agent", "Agent", NewAgentScreen(s, configPath, socketPath))
	s.AddPage("create", "Create", NewCreateScreen(s))
	s.AddPage("edit", "Edit", NewEditScreen(s, socketPath))
	s.AddPage("export", "Export", NewExportScreen(s, socketPath))
	s.AddWidget("daemon-status", "stopped")
	return s
}
