package tuittest

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/theme"
	"github.com/ollykeran/sshush/internal/tui"
)

// Harness drives a Skeleton via Update injection (Bubbletea v2 model tests).
// Use from external packages (not from package tui tests) to avoid import cycles.
type Harness struct {
	T  *testing.T
	Sk *tui.Skeleton
}

// NewAgentHarness builds a Skeleton with a single Agent tab for message-driven tests.
func NewAgentHarness(t *testing.T, socketPath string) (*Harness, *tui.AgentScreen) {
	t.Helper()
	sk := tui.NewSkeleton()
	_ = sk.SetTheme(theme.DefaultTheme())
	agent := tui.NewAgentScreen(sk, "", socketPath)
	sk.AddPage("agent", "Agent", agent)
	return &Harness{T: t, Sk: sk}, agent
}

// Send applies msg to the skeleton and stores the updated model.
func (h *Harness) Send(msg tea.Msg) tea.Cmd {
	h.T.Helper()
	updated, cmd := h.Sk.Update(msg)
	sk, ok := updated.(*tui.Skeleton)
	if !ok {
		h.T.Fatalf("Update returned %T, want *Skeleton", updated)
	}
	h.Sk = sk
	return cmd
}

// Resize sends a WindowSizeMsg.
func (h *Harness) Resize(w, hgt int) tea.Cmd {
	return h.Send(tea.WindowSizeMsg{Width: w, Height: hgt})
}

// Key sends a KeyPressMsg by rune (for simple printable keys).
func (h *Harness) Key(r rune) tea.Cmd {
	return h.Send(tea.KeyPressMsg{Code: r})
}

// WaitForZone polls bubblezone until id is registered or times out.
func WaitForZone(id string) *zone.ZoneInfo {
	for i := 0; i < 50; i++ {
		if z := zone.Get(id); z != nil {
			return z
		}
		time.Sleep(2 * time.Millisecond)
	}
	return nil
}
