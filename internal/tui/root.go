package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ollykeran/sshush/internal/theme"
)

// ButtonFlashDoneMsg is sent when a button press flash animation completes.
type ButtonFlashDoneMsg struct{}

// ButtonFlashCmd returns a tea.Cmd that sends ButtonFlashDoneMsg after 200ms.
func ButtonFlashCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg {
		return ButtonFlashDoneMsg{}
	})
}

// NavToTabBarMsg moves focus back to the tab bar.
type NavToTabBarMsg struct{}

// ThemeChangedMsg is sent after the theme is updated so screens can refresh styled components (e.g. KeyTable).
type ThemeChangedMsg struct{}

// ThemeMessageClearMsg is sent after the temporary footer theme message times out.
// Generation is used so an old timeout does not clear a message that was overridden.
type ThemeMessageClearMsg struct{ Generation int }

func navToTabBarCmd() tea.Cmd {
	return func() tea.Msg {
		return NavToTabBarMsg{}
	}
}

func themeChangedCmd() tea.Cmd {
	return func() tea.Msg {
		return ThemeChangedMsg{}
	}
}

// themeMessageTimeoutCmd returns a Cmd that clears the temporary theme footer message
// after 2s. Pass the current theme message generation so an old timeout does not clear
// a message that was overridden by another theme message.
func themeMessageTimeoutCmd(generation int) tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return ThemeMessageClearMsg{Generation: generation}
	})
}

// NewTUI builds the TUI skeleton with agent, create, edit, and export tabs.
// Theme is used for all TUI colours; pass theme.DefaultTheme() or from config.
// configPath is used by the theme picker to persist theme changes.
// agentBackendMode is "vault" or "keys" (from config.AgentBackendMode); shown in the footer.
func NewTUI(configPath, socketPath string, t theme.Theme, agentBackendMode string) *Skeleton {
	s := NewSkeleton()
	s.theme = t
	s.styles = BuildStyles(t)
	s.configPath = configPath
	s.agentBackendMode = agentBackendMode
	s.KeyMap.SwitchTabLeft = []string{"left", "h"}
	s.KeyMap.SwitchTabRight = []string{"right", "l"}

	s.AddPage("agent", "Agent", NewAgentScreen(s, configPath, socketPath))
	s.AddPage("create", "Create", NewCreateScreen(s))
	s.AddPage("edit", "Edit", NewEditScreen(s, socketPath))
	s.AddPage("export", "Export", NewExportScreen(s, socketPath))
	s.AddWidget("sshushd", "stopped")
	return s
}
