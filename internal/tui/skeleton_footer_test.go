package tui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/theme"
)

type footerTestModel struct{}

func (m footerTestModel) Init() tea.Cmd                       { return nil }
func (m footerTestModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (m footerTestModel) View() tea.View                      { return tea.NewView("") }

func TestMain(m *testing.M) {
	zone.NewGlobal()
	os.Exit(m.Run())
}

func newFooterTestSkeleton() *Skeleton {
	sk := NewSkeleton()
	sk.theme = theme.DefaultTheme()
	sk.styles = BuildStyles(sk.theme)
	sk.AddPage("test", "Test", footerTestModel{})
	return sk
}

func TestRenderOuterFooterVaultPadlock(t *testing.T) {
	sk := newFooterTestSkeleton()
	sk.agentBackendMode = "vault"

	tests := []struct {
		name     string
		mode     string
		locked   bool
		known    bool
		wantLock string // padlock segment when locked; "" if no padlock
		wantOpen string // padlock segment when unlocked; "" if no padlock
	}{
		{name: "unknown state shows mode without padlock", mode: "", locked: false, known: false, wantLock: "", wantOpen: ""},
		{name: "locked vault shows warn padlock", mode: "vault", locked: true, known: true, wantLock: "🔒 vault locked", wantOpen: ""},
		{name: "unlocked vault shows open padlock", mode: "vault", locked: false, known: true, wantLock: "", wantOpen: "🔓 vault"},
		{name: "keys mode shows no padlock", mode: "keys", locked: false, known: true, wantLock: "", wantOpen: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sk.UpdateVaultState(tt.mode, tt.locked, tt.known)
			foot := sk.renderOuterFooter(120)

			if got := strings.Contains(foot, "🔒"); got != (tt.wantLock != "") {
				t.Errorf("locked padlock present=%v want=%v\nfooter=%q", got, tt.wantLock != "", foot)
			}
			if got := strings.Contains(foot, "🔓"); got != (tt.wantOpen != "") {
				t.Errorf("open padlock present=%v want=%v\nfooter=%q", got, tt.wantOpen != "", foot)
			}
			if tt.wantLock != "" && !strings.Contains(foot, tt.wantLock) {
				t.Errorf("footer missing %q\nfooter=%q", tt.wantLock, foot)
			}
			if tt.wantOpen != "" && !strings.Contains(foot, tt.wantOpen) {
				t.Errorf("footer missing %q\nfooter=%q", tt.wantOpen, foot)
			}
		})
	}
}

func TestRenderOuterFooterModeMovedToLeft(t *testing.T) {
	sk := newFooterTestSkeleton()
	sk.agentBackendMode = "vault"
	sk.UpdateVaultState("vault", false, true)

	foot := sk.renderOuterFooter(120)
	// The mode label should appear exactly once: in the left segment, not on the right.
	if got := strings.Count(foot, "vault"); got != 1 {
		t.Errorf("mode label 'vault' count = %d, want 1\nfooter=%q", got, foot)
	}
	// Footer chrome should still be present on the right.
	for _, want := range []string{"[t] theme", "[?] help"} {
		if !strings.Contains(foot, want) {
			t.Errorf("footer missing %q\nfooter=%q", want, foot)
		}
	}
}

func TestAgentScreenVaultLockedStatus(t *testing.T) {
	sk := NewSkeleton()
	sk.theme = theme.DefaultTheme()
	sk.styles = BuildStyles(sk.theme)
	screen := NewAgentScreen(sk, "", "/tmp/agent.sock")

	updated, cmd := screen.Update(vaultStatusMsg{mode: "vault", reachable: true, locked: true})
	sc := updated.(*AgentScreen)
	if cmd != nil {
		if m := cmd(); m != nil {
			sk.Update(m)
		}
	}
	if !sc.vaultLocked || !sc.vaultKnown {
		t.Fatalf("vaultLocked=%v vaultKnown=%v, want true/true", sc.vaultLocked, sc.vaultKnown)
	}
	if sc.status != "vault locked - press u to unlock" || !sc.statusErr {
		t.Errorf("status=%q err=%v, want locked warning", sc.status, sc.statusErr)
	}
	if !sc.sk.vaultLocked || !sc.sk.vaultKnown || sc.sk.vaultMode != "vault" {
		t.Errorf("skeleton vault state not synced: %+v", sc.sk)
	}

	// A keys refresh while locked must keep the warning.
	updated, _ = sc.Update(agentKeysMsg{refresh: true})
	sc = updated.(*AgentScreen)
	if sc.status != "vault locked - press u to unlock" || !sc.statusErr {
		t.Errorf("keys refresh while locked: status=%q err=%v", sc.status, sc.statusErr)
	}
}

func TestAgentScreenUnlockResetsCachedState(t *testing.T) {
	sk := NewSkeleton()
	sk.theme = theme.DefaultTheme()
	sk.styles = BuildStyles(sk.theme)
	screen := NewAgentScreen(sk, "", "/tmp/agent.sock")

	sc := screen
	sc.vaultKnown = true
	sc.vaultLocked = true

	updated, _ := sc.Update(agentUnlockResultMsg{})
	sc = updated.(*AgentScreen)
	if sc.vaultKnown || sc.vaultLocked {
		t.Errorf("after unlock result vaultKnown=%v vaultLocked=%v, want false/false", sc.vaultKnown, sc.vaultLocked)
	}
	if sc.status != "agent unlocked" || sc.statusErr {
		t.Errorf("status=%q err=%v, want 'agent unlocked'", sc.status, sc.statusErr)
	}
}

func TestAgentScreenVaultPollGeneration(t *testing.T) {
	sk := NewSkeleton()
	screen := NewAgentScreen(sk, "", "/tmp/agent.sock")

	updated, cmd := screen.Update(agentDaemonStateMsg{running: true})
	sc := updated.(*AgentScreen)
	if cmd == nil {
		t.Fatal("daemon start should arm a vault poll")
	}
	if sc.vaultPollGen != 1 {
		t.Fatalf("vaultPollGen=%d, want 1", sc.vaultPollGen)
	}

	// A stale poll chain is dropped.
	_, cmd = sc.Update(vaultPollMsg{gen: 0})
	if cmd != nil {
		t.Fatalf("stale poll produced cmd: %v", cmd)
	}

	// The current poll chain re-arms.
	updated, cmd = sc.Update(vaultPollMsg{gen: 1})
	if cmd == nil {
		t.Fatal("current poll should re-arm")
	}
	sc = updated.(*AgentScreen)

	// Daemon stop clears vault state and bumps the generation.
	updated, cmd = sc.Update(agentDaemonStateMsg{running: false})
	sc = updated.(*AgentScreen)
	if cmd != nil {
		if m := cmd(); m != nil {
			sk.Update(m)
		}
	}
	if sc.vaultKnown || sc.vaultLocked {
		t.Errorf("after stop vaultKnown=%v vaultLocked=%v, want false/false", sc.vaultKnown, sc.vaultLocked)
	}
	if sc.vaultPollGen != 2 {
		t.Errorf("vaultPollGen=%d, want 2", sc.vaultPollGen)
	}
	if sc.sk.vaultKnown || sc.sk.vaultLocked {
		t.Errorf("skeleton vault state not cleared after stop: %+v", sc.sk)
	}
}

func TestAgentScreenLockUnlockButtons(t *testing.T) {
	sk := NewSkeleton()
	sk.theme = theme.DefaultTheme()
	sk.styles = BuildStyles(sk.theme)
	screen := NewAgentScreen(sk, "", "/tmp/agent.sock")

	labels := strings.Join(screen.buttons.Labels, ",")
	if !strings.Contains(labels, "[L]ock") || !strings.Contains(labels, "[u]nlock") {
		t.Fatalf("button labels %q missing lock/unlock", labels)
	}

	_, cmd := screen.pressButton(btnLock)
	if !screen.showPass || screen.passAction != "lock" {
		t.Errorf("after lock press showPass=%v action=%q, want true/'lock'", screen.showPass, screen.passAction)
	}
	if cmd == nil {
		t.Error("lock press should return a focus cmd")
	}
	if screen.buttons.Pressed != -1 {
		t.Errorf("lock press should not flash button, Pressed=%d", screen.buttons.Pressed)
	}

	sc := NewAgentScreen(sk, "", "/tmp/agent.sock")
	_, cmd = sc.pressButton(btnUnlock)
	if !sc.showPass || sc.passAction != "unlock" {
		t.Errorf("after unlock press showPass=%v action=%q, want true/'unlock'", sc.showPass, sc.passAction)
	}
	if cmd == nil {
		t.Error("unlock press should return a focus cmd")
	}

	sc = NewAgentScreen(sk, "", "/tmp/agent.sock")
	_, cmd = sc.pressButton(btnStart)
	if sc.showPass {
		t.Error("start press should not open the passphrase modal")
	}
	if sc.status != "starting..." {
		t.Errorf("status=%q, want 'starting...'", sc.status)
	}
	if sc.buttons.Pressed != btnStart {
		t.Errorf("start press should flash button, Pressed=%d", sc.buttons.Pressed)
	}
	if cmd == nil {
		t.Error("start press should return a cmd")
	}
}

func TestAgentScreenLockVaultModeNeedsNoPassphrase(t *testing.T) {
	sk := NewSkeleton()
	sk.theme = theme.DefaultTheme()
	sk.styles = BuildStyles(sk.theme)

	// Vault mode: lock is immediate, no passphrase prompt.
	agent := NewAgentScreen(sk, "", "/tmp/agent.sock")
	agent.vaultMode = "vault"
	_, cmd := agent.pressButton(btnLock)
	if agent.showPass {
		t.Error("vault-mode lock should not open the passphrase prompt")
	}
	if agent.passAction != "" {
		t.Errorf("passAction=%q, want empty", agent.passAction)
	}
	if agent.status != "locking..." {
		t.Errorf("status=%q, want 'locking...'", agent.status)
	}
	if cmd == nil {
		t.Error("vault-mode lock should return a lock cmd")
	}
	if agent.buttons.Pressed != -1 {
		t.Errorf("vault-mode lock should not flash button, Pressed=%d", agent.buttons.Pressed)
	}

	// Keys mode (or unknown mode): lock still needs a passphrase prompt.
	agent = NewAgentScreen(sk, "", "/tmp/agent.sock")
	agent.vaultMode = "keys"
	_, cmd = agent.pressButton(btnLock)
	if !agent.showPass || agent.passAction != "lock" {
		t.Errorf("keys-mode lock: showPass=%v action=%q, want true/'lock'", agent.showPass, agent.passAction)
	}
	if cmd == nil {
		t.Error("keys-mode lock should return a focus cmd")
	}
}

func TestSkeletonDaemonFocusLockUnlock(t *testing.T) {
	sk := NewSkeleton()
	sk.theme = theme.DefaultTheme()
	sk.styles = BuildStyles(sk.theme)
	agent := NewAgentScreen(sk, "", "/tmp/agent.sock")
	sk.AddPage("agent", "Agent", agent)

	for _, tc := range []struct {
		key    rune
		action string
	}{
		{key: 'L', action: "lock"},
		{key: 'u', action: "unlock"},
	} {
		t.Run(string(tc.key), func(t *testing.T) {
			agent.showPass = false
			sk.navFocus = navFocusDaemon
			_, cmd := sk.Update(tea.KeyPressMsg{Code: tc.key})
			if cmd == nil {
				t.Fatal("daemon-focus hotkey should return a focus cmd")
			}
			if sk.navFocus != navFocusScreen {
				t.Errorf("navFocus=%v, want navFocusScreen", sk.navFocus)
			}
			if !agent.showPass || agent.passAction != tc.action {
				t.Errorf("showPass=%v action=%q, want true/%q", agent.showPass, agent.passAction, tc.action)
			}
		})
	}
}

func TestSkeletonGlobalLockUnlockHotkeys(t *testing.T) {
	sk := NewSkeleton()
	sk.theme = theme.DefaultTheme()
	sk.styles = BuildStyles(sk.theme)
	agent := NewAgentScreen(sk, "", "/tmp/agent.sock")
	sk.AddPage("agent", "Agent", agent)
	sk.AddPage("create", "Create", NewCreateScreen(sk))

	// From the nav bar on the agent tab.
	sk.activeTab = 0
	sk.navFocus = navFocusTabs
	_, cmd := sk.Update(tea.KeyPressMsg{Code: 'L'})
	if cmd == nil {
		t.Fatal("global L from nav bar should return a focus cmd")
	}
	if !agent.showPass || agent.passAction != "lock" {
		t.Errorf("global L: showPass=%v action=%q, want true/'lock'", agent.showPass, agent.passAction)
	}
	if sk.navFocus != navFocusScreen {
		t.Errorf("global L: navFocus=%v, want navFocusScreen", sk.navFocus)
	}

	// From another tab, focus in the nav bar.
	agent.showPass = false
	sk.activeTab = 1
	sk.navFocus = navFocusTabs
	_, cmd = sk.Update(tea.KeyPressMsg{Code: 'u'})
	if cmd == nil {
		t.Fatal("global u from another tab should return a focus cmd")
	}
	if sk.activeTab != 0 {
		t.Errorf("global u: activeTab=%d, want 0", sk.activeTab)
	}
	if !agent.showPass || agent.passAction != "unlock" {
		t.Errorf("global u: showPass=%v action=%q, want true/'unlock'", agent.showPass, agent.passAction)
	}
	if sk.navFocus != navFocusScreen {
		t.Errorf("global u: navFocus=%v, want navFocusScreen", sk.navFocus)
	}
}

func TestSkeletonDaemonFocusEnterLockButton(t *testing.T) {
	sk := NewSkeleton()
	sk.theme = theme.DefaultTheme()
	sk.styles = BuildStyles(sk.theme)
	agent := NewAgentScreen(sk, "", "/tmp/agent.sock")
	sk.AddPage("agent", "Agent", agent)
	sk.navFocus = navFocusDaemon
	agent.buttons.Active = btnLock

	_, cmd := sk.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on lock button should return a focus cmd")
	}
	if sk.navFocus != navFocusScreen {
		t.Errorf("navFocus=%v, want navFocusScreen", sk.navFocus)
	}
	if !agent.showPass || agent.passAction != "lock" {
		t.Errorf("showPass=%v action=%q, want true/'lock'", agent.showPass, agent.passAction)
	}
}
