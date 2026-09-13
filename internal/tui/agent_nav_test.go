package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/theme"
	"github.com/ollykeran/sshush/internal/utils"
)

func waitForZone(id string) *zone.ZoneInfo { return WaitForZone(id) }

func newAgentTestSkeleton() (*Skeleton, *AgentScreen) { return NewAgentTestSkeleton() }

func seedAgentKeyRows(agent *AgentScreen, n int) { SeedAgentKeyRows(agent, n) }

func TestAgentRowHighlightSpansAllColumns(t *testing.T) {
	st := BuildStyles(theme.DefaultTheme())
	kt := NewKeyTable(80, 5, st)
	kt.SetRows([]table.Row{{"ssh-ed25519", "SHA256:aaa", "k1"}})
	kt.Table.SetCursor(0)

	view := kt.AgentView(true, st)
	bg := "48;2;" // lipgloss truecolor background prefix
	typeIdx := strings.Index(view, "ssh-ed25519")
	fpIdx := strings.Index(view, "SHA256:aaa")
	if typeIdx < 0 || fpIdx < 0 {
		t.Fatal("expected type and fingerprint in view")
	}
	typeBg := strings.LastIndex(view[:typeIdx], bg)
	fpBg := strings.LastIndex(view[:fpIdx], bg)
	if typeBg < 0 {
		t.Fatal("expected background on type cell")
	}
	if fpBg < 0 {
		t.Fatal("expected background on fingerprint cell")
	}
}

func TestAgentKeysMsgAfterEmptyShowsRow(t *testing.T) {
	_, agent := newAgentTestSkeleton()
	agent.width = 80
	agent.focus = agentFocusFound
	agent.keyTable.SetRows(nil)
	agent.keyTable.SetSize(agent.width, agent.loadedKeysTableHeight(), agent.sk.Styles())

	updated, _ := agent.Update(agentKeysMsg{})
	agent = updated.(*AgentScreen)

	updated, _ = agent.Update(agentKeysMsg{
		keys: nil, // simulates refetch after add; rows built from socket in real flow
	})
	agent = updated.(*AgentScreen)
	agent.keyTable.SetRows([]table.Row{{"ssh-ed25519", "SHA256:fp0", "k0"}})
	agent.keyTable.SetSize(agent.width, agent.loadedKeysTableHeight(), agent.sk.Styles())
	agent.syncTableSelection()

	if agent.keyTable.Table.Cursor() < 0 {
		t.Fatalf("cursor=%d, want >= 0 so rows render", agent.keyTable.Table.Cursor())
	}
	if !strings.Contains(agent.View().Content, "SHA256:fp0") {
		t.Fatal("expected added key visible before focus change")
	}
}

func TestAgentTableSelectionHiddenWhenFoundFocused(t *testing.T) {
	_, agent := newAgentTestSkeleton()
	seedAgentKeyRows(agent, 1)
	agent.focus = agentFocusTable
	agent.syncTableSelection()
	viewFocused := agent.keyTable.AgentView(true, agent.sk.Styles())

	agent.focus = agentFocusFound
	agent.syncTableSelection()
	viewUnfocused := agent.keyTable.AgentView(false, agent.sk.Styles())

	if viewFocused == viewUnfocused {
		t.Fatal("table view should differ when row selection highlight toggles")
	}
}

func TestSkeletonEnterAgentFocusesFirstKey(t *testing.T) {
	sk, agent := newAgentTestSkeleton()
	seedAgentKeyRows(agent, 3)
	agent.keyTable.Table.SetCursor(2)
	sk.navFocus = navFocusTabs

	_, _ = sk.Update(tea.KeyPressMsg{Code: 'j'})
	if sk.navFocus != navFocusScreen {
		t.Fatalf("navFocus=%v, want navFocusScreen", sk.navFocus)
	}
	if agent.focus != agentFocusTable {
		t.Fatalf("focus=%d, want agentFocusTable", agent.focus)
	}
	if agent.keyTable.Table.Cursor() != 0 {
		t.Fatalf("cursor=%d, want 0 on screen entry", agent.keyTable.Table.Cursor())
	}
}

func TestAgentLoadedKeysTableHeightFitsRows(t *testing.T) {
	_, agent := newAgentTestSkeleton()
	seedAgentKeyRows(agent, 2)
	got := agent.loadedKeysTableHeight()
	want := 4 // header + rule + 2 rows
	if got != want {
		t.Fatalf("height=%d, want %d", got, want)
	}
}

func TestAgentTableCursorMovesDown(t *testing.T) {
	_, agent := newAgentTestSkeleton()
	agent.focus = agentFocusTable
	seedAgentKeyRows(agent, 3)
	agent.keyTable.Table.SetCursor(0)

	_, _ = agent.Update(tea.KeyPressMsg{Code: 'j'})
	if agent.keyTable.Table.Cursor() != 1 {
		t.Fatalf("cursor=%d, want 1 after j", agent.keyTable.Table.Cursor())
	}
	if agent.focus != agentFocusTable {
		t.Fatalf("focus=%d, want agentFocusTable", agent.focus)
	}
}

func TestAgentTableCursorMovesUp(t *testing.T) {
	_, agent := newAgentTestSkeleton()
	agent.focus = agentFocusTable
	seedAgentKeyRows(agent, 3)
	agent.keyTable.Table.SetCursor(2)

	_, _ = agent.Update(tea.KeyPressMsg{Code: 'k'})
	if agent.keyTable.Table.Cursor() != 1 {
		t.Fatalf("cursor=%d, want 1 after k", agent.keyTable.Table.Cursor())
	}
}

func TestAgentTableDownAtLastRowEntersFoundKeys(t *testing.T) {
	_, agent := newAgentTestSkeleton()
	agent.focus = agentFocusTable
	seedAgentKeyRows(agent, 3)
	agent.keyTable.Table.SetCursor(2)
	agent.foundKeys = []utils.KeyPath{{Path: "/tmp/id_ed25519"}}
	agent.loadedFPs = map[string]bool{}

	_, _ = agent.Update(tea.KeyPressMsg{Code: 'j'})
	if agent.focus != agentFocusFound {
		t.Fatalf("focus=%d, want agentFocusFound", agent.focus)
	}
	if agent.foundSelected != 0 {
		t.Fatalf("foundSelected=%d, want 0", agent.foundSelected)
	}
}

func TestAgentFoundKeysUpReturnsToTable(t *testing.T) {
	_, agent := newAgentTestSkeleton()
	agent.focus = agentFocusFound
	agent.foundSelected = 0
	seedAgentKeyRows(agent, 2)

	_, _ = agent.Update(tea.KeyPressMsg{Code: 'k'})
	if agent.focus != agentFocusTable {
		t.Fatalf("focus=%d, want agentFocusTable", agent.focus)
	}
}

func TestAgentTableUpAtFirstRowNavigatesToTabs(t *testing.T) {
	sk, agent := newAgentTestSkeleton()
	agent.focus = agentFocusTable
	seedAgentKeyRows(agent, 2)
	agent.keyTable.Table.SetCursor(0)

	_, cmd := agent.Update(tea.KeyPressMsg{Code: 'k'})
	if cmd == nil {
		t.Fatal("expected navToTabBar cmd")
	}
	msg := cmd()
	if _, ok := msg.(NavToTabBarMsg); !ok {
		t.Fatalf("cmd msg type=%T, want NavToTabBarMsg", msg)
	}
	updated, _ := sk.Update(msg)
	sk = updated.(*Skeleton)
	if sk.navFocus != navFocusTabs {
		t.Fatalf("navFocus=%v, want navFocusTabs", sk.navFocus)
	}
}

func TestAgentRemoveSelectedKeyUsesCursorRow(t *testing.T) {
	_, agent := newAgentTestSkeleton()
	agent.focus = agentFocusTable
	seedAgentKeyRows(agent, 2)
	agent.keyTable.Table.SetCursor(1)

	_, cmd := agent.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if cmd == nil {
		t.Fatal("expected remove cmd")
	}
	msg := cmd()
	status, ok := msg.(agentStatusMsg)
	if !ok {
		t.Fatalf("cmd msg type=%T, want agentStatusMsg", msg)
	}
	if status.isErr && status.text != "agent not running" {
		t.Fatalf("unexpected status: %q err=%v", status.text, status.isErr)
	}
}

func TestAgentRemoveEmptyTableNoOp(t *testing.T) {
	_, agent := newAgentTestSkeleton()
	agent.focus = agentFocusTable
	agent.keyTable.SetRows(nil)

	_, cmd := agent.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if cmd != nil {
		t.Fatal("expected nil cmd for empty table remove")
	}
}

func TestSkeletonDRemovesKeyOnAgentTable(t *testing.T) {
	sk, agent := newAgentTestSkeleton()
	agent.focus = agentFocusTable
	seedAgentKeyRows(agent, 2)
	agent.keyTable.Table.SetCursor(0)

	_, cmd := sk.Update(tea.KeyPressMsg{Code: 'd'})
	if sk.navFocus == navFocusHeaderTools || agent.HeaderToolsFocused() {
		t.Fatal("d on agent table should not enter header tools focus")
	}
	if cmd == nil {
		t.Fatal("expected remove cmd from skeleton d routing")
	}
}

func TestSkeletonDEntersDaemonOffAgentTable(t *testing.T) {
	sk, agent := newAgentTestSkeleton()
	agent.focus = agentFocusFound
	seedAgentKeyRows(agent, 2)

	_, _ = sk.Update(tea.KeyPressMsg{Code: 'd'})
	if sk.navFocus != navFocusHeaderTools {
		t.Fatalf("navFocus=%v, want navFocusHeaderTools when not on table", sk.navFocus)
	}
	if !agent.HeaderToolsFocused() {
		t.Fatal("expected agent header tools focused")
	}
}

func TestSkeletonDEntersDaemonOnOtherTab(t *testing.T) {
	sk, agent := newAgentTestSkeleton()
	sk.AddPage("create", "Create", NewCreateScreen(sk))
	sk.activeTab = 1
	sk.navFocus = navFocusScreen

	_, _ = sk.Update(tea.KeyPressMsg{Code: 'd'})
	if sk.navFocus != navFocusHeaderTools {
		t.Fatalf("navFocus=%v, want navFocusHeaderTools on non-agent tab", sk.navFocus)
	}
	if !agent.HeaderToolsFocused() {
		t.Fatal("expected agent header tools focused")
	}
}

func TestLoadedKeyRowZonesRegistered(t *testing.T) {
	_, agent := newAgentTestSkeleton()
	agent.width = 80
	agent.height = 24
	longFP := "SHA256:abcdefghijklmnopqrstuvwxyz0123456789/+abcdefghijklmnopqrstuvwxyz"
	agent.keyTable.SetRows([]table.Row{{"ssh-ed25519", longFP, "mykey"}})
	agent.keyTable.SetSize(agent.width, agent.loadedKeysTableHeight(), agent.sk.Styles())
	agent.focus = agentFocusTable

	zone.Scan(agent.View().Content)
	if waitForZone(agent.zonePrefix+"key-0") == nil {
		t.Fatal("key-0 zone not registered (long fingerprint truncates in view)")
	}
}

func TestAgentMouseClickSelectsTableRow(t *testing.T) {
	_, agent := newAgentTestSkeleton()
	agent.width = 80
	agent.height = 24
	seedAgentKeyRows(agent, 3)
	agent.keyTable.Table.SetCursor(0)

	zone.Scan(agent.View().Content)
	wantRow := 2
	z := waitForZone(fmt.Sprintf("%skey-%d", agent.zonePrefix, wantRow))
	if z == nil {
		t.Fatalf("row zone %skey-%d not registered", agent.zonePrefix, wantRow)
	}
	x := (z.StartX + z.EndX) / 2
	clickY := z.StartY

	updated, cmd := agent.Update(tea.MouseClickMsg{X: x, Y: clickY, Button: tea.MouseLeft})
	agent = updated.(*AgentScreen)
	if cmd != nil {
		t.Fatal("mouse click should not trigger an action cmd")
	}
	if agent.focus != agentFocusTable {
		t.Fatalf("focus=%d, want agentFocusTable after click", agent.focus)
	}
	if agent.keyTable.Table.Cursor() != wantRow {
		t.Fatalf("cursor=%d, want %d after click", agent.keyTable.Table.Cursor(), wantRow)
	}
}

func TestAgentMouseSelectsTableRow(t *testing.T) {
	_, agent := newAgentTestSkeleton()
	agent.width = 80
	agent.height = 24
	seedAgentKeyRows(agent, 3)
	agent.keyTable.Table.SetCursor(0)

	zone.Scan(agent.View().Content)

	wantRow := 1
	z := waitForZone(fmt.Sprintf("%skey-%d", agent.zonePrefix, wantRow))
	if z == nil {
		t.Fatalf("row zone %skey-%d not registered", agent.zonePrefix, wantRow)
	}
	x := (z.StartX + z.EndX) / 2
	clickY := z.StartY

	updated, cmd := agent.handleMouse(x, clickY)
	agent = updated.(*AgentScreen)
	if cmd != nil {
		t.Fatal("mouse select should not trigger an action cmd")
	}
	if agent.focus != agentFocusTable {
		t.Fatalf("focus=%d, want agentFocusTable after click", agent.focus)
	}
	if agent.keyTable.Table.Cursor() != wantRow {
		t.Fatalf("cursor=%d, want %d after click", agent.keyTable.Table.Cursor(), wantRow)
	}
}

func TestAgentFoundKeysSelectionClampedToVisible(t *testing.T) {
	_, agent := newAgentTestSkeleton()
	agent.focus = agentFocusFound
	agent.foundKeys = make([]utils.KeyPath, 10)
	for i := range agent.foundKeys {
		agent.foundKeys[i] = utils.KeyPath{Path: fmt.Sprintf("/tmp/key%d", i)}
	}
	agent.loadedFPs = map[string]bool{}
	agent.foundSelected = foundKeysMaxVisible - 1

	_, _ = agent.Update(tea.KeyPressMsg{Code: 'j'})
	if agent.foundSelected != foundKeysMaxVisible-1 {
		t.Fatalf("foundSelected=%d, want clamp at %d", agent.foundSelected, foundKeysMaxVisible-1)
	}
}
