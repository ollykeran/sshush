package tui

import (
	tea "charm.land/bubbletea/v2"
)

const agentPageID = "agent"

// HeaderToolsView renders daemon control buttons for the Skeleton header slot.
func (s *AgentScreen) HeaderToolsView(focused bool) string {
	return s.ControlButtonsInlineView(focused)
}

// HeaderToolsFocused reports whether daemon controls own keyboard focus.
func (s *AgentScreen) HeaderToolsFocused() bool {
	return s.focus == agentFocusButtons
}

// HeaderToolsUpdate handles keys while header tools are focused.
func (s *AgentScreen) HeaderToolsUpdate(msg tea.Msg) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	key := keyMsg.String()
	switch {
	case KeyMatchesString(key, KeyDaemonFocus) || KeyMatchesString(key, KeyEsc):
		s.buttons.Active = 0
		s.focus = agentFocusTable
		return func() tea.Msg { return ExitHeaderToolsMsg{} }
	case KeyMatchesString(key, KeyLeft):
		if s.buttons.Active > 0 {
			s.buttons.Left()
		}
		return nil
	case KeyMatchesString(key, KeyRight):
		if s.buttons.Active < len(s.buttons.Labels)-1 {
			s.buttons.Right()
		}
		return nil
	case KeyMatchesString(key, KeyEnter):
		_, cmd := s.pressButton(s.buttons.Active)
		return cmd
	case KeyMatchesString(key, KeyDaemonStart):
		_, cmd := s.pressButton(btnStart)
		return cmd
	case KeyMatchesString(key, KeyDaemonStop):
		_, cmd := s.pressButton(btnStop)
		return cmd
	case KeyMatchesString(key, KeyDaemonReload):
		_, cmd := s.pressButton(btnReload)
		return cmd
	case KeyMatchesString(key, KeyDaemonLock):
		return s.startLock()
	case KeyMatchesString(key, KeyDaemonUnlock):
		return s.startPassphrase("unlock")
	}
	return nil
}

// HeaderToolsHandleMouse handles clicks on daemon control buttons.
func (s *AgentScreen) HeaderToolsHandleMouse(x, y int) (bool, tea.Cmd) {
	btn := s.buttons.HandleMouse(x, y)
	if btn < 0 {
		return false, nil
	}
	s.buttons.Active = btn
	_, cmd := s.pressButton(btn)
	if !s.HasModal() {
		s.focus = agentFocusButtons
	}
	return true, cmd
}

// HandleGlobalKey implements GlobalHotkeys for s/x/r/L/u/d.
func (s *AgentScreen) HandleGlobalKey(key string, pageActive bool) (bool, tea.Cmd, tea.Msg) {
	switch {
	case KeyMatchesString(key, KeyDaemonStart):
		_, cmd := s.pressButton(btnStart)
		return true, cmd, nil
	case KeyMatchesString(key, KeyDaemonStop):
		_, cmd := s.pressButton(btnStop)
		return true, cmd, nil
	case KeyMatchesString(key, KeyDaemonReload):
		_, cmd := s.pressButton(btnReload)
		return true, cmd, nil
	case KeyMatchesString(key, KeyDaemonLock):
		cmd := s.startLock()
		return true, cmd, ActivateHeaderToolsMsg{PageID: agentPageID, EnsureOnly: true, ScreenFocus: s.HasModal()}
	case KeyMatchesString(key, KeyDaemonUnlock):
		cmd := s.startPassphrase("unlock")
		return true, cmd, ActivateHeaderToolsMsg{PageID: agentPageID, EnsureOnly: true, ScreenFocus: true}
	case KeyMatchesString(key, KeyDaemonFocus):
		if pageActive && s.focus == agentFocusTable && !s.HasModal() {
			return false, nil, nil // Skeleton forwards to Agent.Update for key remove
		}
		s.focus = agentFocusButtons
		s.buttons.Active = 0
		return true, nil, ActivateHeaderToolsMsg{PageID: agentPageID}
	}
	return false, nil, nil
}

// HandlesAsync claims agent-only async messages for Skeleton routing.
func (s *AgentScreen) HandlesAsync(msg tea.Msg) bool {
	switch msg.(type) {
	case agentStatusMsg, agentKeysMsg, agentDaemonStateMsg, agentLockResultMsg, agentUnlockResultMsg, vaultStatusMsg, vaultPollMsg, foundKeysMsg, ButtonFlashDoneMsg:
		return true
	default:
		return false
	}
}

// OnScreenEnter focuses the loaded-keys table when entering from the tab bar.
func (s *AgentScreen) OnScreenEnter() {
	s.focusFirstLoadedKey()
}
