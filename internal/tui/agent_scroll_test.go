package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/utils"
)

func TestScrollOffset(t *testing.T) {
	tests := []struct {
		name                          string
		offset, cursor, total, height int
		want                          int
	}{
		{"everything fits", 3, 2, 5, 8, 0},
		{"cursor inside window stays put", 2, 4, 20, 5, 2},
		{"cursor below window scrolls down just enough", 0, 7, 20, 5, 3},
		{"cursor above window scrolls up to it", 6, 2, 20, 5, 2},
		{"never past the last full window", 18, 19, 20, 5, 15},
		{"list shrank under the offset", 12, 3, 6, 5, 1},
		{"zero height shows from the top", 4, 4, 10, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scrollOffset(tt.offset, tt.cursor, tt.total, tt.height); got != tt.want {
				t.Fatalf("scrollOffset(%d, %d, %d, %d) = %d, want %d",
					tt.offset, tt.cursor, tt.total, tt.height, got, tt.want)
			}
		})
	}
}

func TestScrollHint(t *testing.T) {
	if got := scrollHint(0, 8, 15); got != "↓ 1–8 of 15" {
		t.Fatalf("top of list: %q", got)
	}
	if got := scrollHint(3, 8, 15); got != "↑↓ 4–11 of 15" {
		t.Fatalf("middle of list: %q", got)
	}
	if got := scrollHint(7, 8, 15); got != "↑ 8–15 of 15" {
		t.Fatalf("bottom of list: %q", got)
	}
}

func seedFoundKeys(agent *AgentScreen, n int) {
	agent.foundKeys = make([]utils.KeyPath, n)
	for i := range agent.foundKeys {
		agent.foundKeys[i] = utils.KeyPath{Path: fmt.Sprintf("/tmp/key%d", i)}
	}
	agent.loadedFPs = map[string]bool{}
}

func agentPlainView(agent *AgentScreen) string {
	return ansi.Strip(agent.View().Content)
}

// The Loaded Keys table draws a window of rows that follows the cursor, rather
// than every row, so a long key list no longer runs off the bottom of the page.
func TestAgentLoadedKeysScrollWithCursor(t *testing.T) {
	_, agent := newAgentTestSkeleton()
	agent.width, agent.height = 100, 40
	seedAgentKeyRows(agent, 15)
	agent.keyTable.Table.SetCursor(0)
	agent.focus = agentFocusTable

	view := agentPlainView(agent)
	shown := agent.loadedKeysRows()
	if shown >= 15 {
		t.Fatalf("loadedKeysRows=%d; want fewer than the 15 keys so the table scrolls", shown)
	}
	if !strings.Contains(view, "fp0") || strings.Contains(view, "fp14") {
		t.Fatalf("top of list should show fp0 and not fp14:\n%s", view)
	}
	if want := fmt.Sprintf("↓ 1–%d of 15", shown); !strings.Contains(view, want) {
		t.Fatalf("missing scroll hint %q:\n%s", want, view)
	}

	for range 14 {
		_, _ = agent.Update(tea.KeyPressMsg{Code: 'j'})
	}
	if c := agent.keyTable.Table.Cursor(); c != 14 {
		t.Fatalf("cursor=%d, want 14", c)
	}
	view = agentPlainView(agent)
	if !strings.Contains(view, "fp14") || strings.Contains(view, "fp3") {
		t.Fatalf("after scrolling down, fp14 should show and fp3 should not:\n%s", view)
	}
	if want := fmt.Sprintf("↑ %d–15 of 15", 15-shown+1); !strings.Contains(view, want) {
		t.Fatalf("missing scroll hint %q:\n%s", want, view)
	}
	if lines := strings.Count(view, "\n") + 1; lines > agent.height {
		t.Fatalf("view is %d lines, taller than the %d-line page", lines, agent.height)
	}
}

// Found Keys shows at least foundKeysMinVisible rows and grows to fill the
// space under Loaded Keys on a taller screen.
func TestAgentFoundKeysFillScreenHeight(t *testing.T) {
	_, agent := newAgentTestSkeleton()
	agent.width = 100
	seedAgentKeyRows(agent, 2)
	seedFoundKeys(agent, 40)

	agent.height = 10
	if got := agent.foundKeysRows(40); got != foundKeysMinVisible {
		t.Fatalf("short screen: foundKeysRows=%d, want the minimum %d", got, foundKeysMinVisible)
	}

	agent.height = 24
	small := agent.foundKeysRows(40)
	agent.height = 50
	large := agent.foundKeysRows(40)
	if small <= foundKeysMinVisible || large <= small {
		t.Fatalf("rows should grow with the screen: 24 lines -> %d, 50 lines -> %d", small, large)
	}
	if got := agent.foundKeysRows(3); got != 3 {
		t.Fatalf("a short list is shown whole: foundKeysRows(3)=%d", got)
	}

	// The page still fits: Loaded Keys, Found Keys and the scroll hint.
	view := agentPlainView(agent)
	if lines := strings.Count(view, "\n") + 1; lines > agent.height {
		t.Fatalf("view is %d lines, taller than the %d-line page:\n%s", lines, agent.height, view)
	}
	if want := fmt.Sprintf("↓ 1–%d of 40", large); !strings.Contains(view, want) {
		t.Fatalf("missing scroll hint %q:\n%s", want, view)
	}
}

// A found key past the first screenful scrolls into view when selected and can
// be clicked to add it.
func TestAgentFoundKeysScrollToSelectionAndClick(t *testing.T) {
	_, agent := newAgentTestSkeleton()
	agent.width, agent.height = 100, 24
	seedFoundKeys(agent, 30)
	agent.focus = agentFocusFound
	agent.foundSelected = 0

	view := agentPlainView(agent)
	if !strings.Contains(view, "/tmp/key0") || strings.Contains(view, "/tmp/key25") {
		t.Fatalf("top of list should show key0 and not key25:\n%s", view)
	}

	for range 25 {
		_, _ = agent.Update(tea.KeyPressMsg{Code: 'j'})
	}
	if agent.foundSelected != 25 {
		t.Fatalf("foundSelected=%d, want 25", agent.foundSelected)
	}
	content := agent.View().Content
	view = ansi.Strip(content)
	if !strings.Contains(view, "/tmp/key25") || strings.Contains(view, "/tmp/key0\n") {
		t.Fatalf("after scrolling, key25 should show and key0 should not:\n%s", view)
	}

	zone.Scan(content)
	z := waitForZone(fmt.Sprintf("%sfound-%d", agent.zonePrefix, 25))
	if z == nil {
		t.Fatal("zone for found key 25 not registered")
	}
	agent.foundSelected = 0
	_, cmd := agent.handleMouse((z.StartX+z.EndX)/2, z.StartY, false)
	if agent.foundSelected != 25 {
		t.Fatalf("click selected %d, want 25", agent.foundSelected)
	}
	if cmd == nil {
		t.Fatal("clicking a found key should start adding it")
	}
}
