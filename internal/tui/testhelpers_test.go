package tui

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/table"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/theme"
)

// NewAgentTestSkeleton builds a minimal Skeleton + AgentScreen for tests.
func NewAgentTestSkeleton() (*Skeleton, *AgentScreen) {
	sk := NewSkeleton()
	sk.theme = theme.DefaultTheme()
	sk.styles = BuildStyles(sk.theme)
	agent := NewAgentScreen(sk, "", "/tmp/agent.sock")
	sk.AddPage("agent", "Agent", agent)
	sk.activeTab = 0
	sk.navFocus = navFocusScreen
	return sk, agent
}

// SeedAgentKeyRows fills the agent key table with n placeholder rows.
func SeedAgentKeyRows(agent *AgentScreen, n int) {
	rows := make([]table.Row, n)
	for i := 0; i < n; i++ {
		rows[i] = table.Row{"ssh-ed25519", fmt.Sprintf("SHA256:fp%d", i), fmt.Sprintf("key%d", i)}
	}
	agent.keyTable.SetRows(rows)
}

// WaitForZone polls bubblezone until id is registered.
func WaitForZone(id string) *zone.ZoneInfo {
	for i := 0; i < 50; i++ {
		if z := zone.Get(id); z != nil {
			return z
		}
		time.Sleep(2 * time.Millisecond)
	}
	return nil
}
