package tui

import tea "charm.land/bubbletea/v2"

// Page is the contract every tab model implements for chrome probes.
type Page interface {
	tea.Model
	HasModal() bool
	HasActiveTextInput() bool
	StatusTextRaw() (string, bool)
}

// HelpProvider is an optional Page that contributes lines to the help overlay.
type HelpProvider interface {
	HelpEntries() []string
}

// HeaderTools is an optional Page that contributes header chrome (e.g. daemon buttons).
type HeaderTools interface {
	HeaderToolsView(focused bool) string
	HeaderToolsFocused() bool
	HeaderToolsUpdate(msg tea.Msg) tea.Cmd
	HeaderToolsHandleMouse(x, y int) (handled bool, cmd tea.Cmd)
}

// GlobalHotkeys is an optional Page that handles app-wide keys (s/x/r/L/u/d).
// pageActive is true when this page is the active tab.
// side, when non-nil, is a chrome message Skeleton applies in the same Update turn.
type GlobalHotkeys interface {
	HandleGlobalKey(key string, pageActive bool) (handled bool, cmd tea.Cmd, side tea.Msg)
}

// AsyncMsgRouter is an optional Page that claims typed async messages.
type AsyncMsgRouter interface {
	HandlesAsync(msg tea.Msg) bool
}

// ScreenEnter is an optional Page hook when focus moves from tabs into the screen.
type ScreenEnter interface {
	OnScreenEnter()
}

// ActivateHeaderToolsMsg asks Skeleton to show/focus header tools (or just ensure a page).
type ActivateHeaderToolsMsg struct {
	PageID      string
	EnsureOnly  bool // if true, switch to page / screen focus without entering tools mode
	ScreenFocus bool // with EnsureOnly: take screen focus (e.g. passphrase modal)
}

// ExitHeaderToolsMsg asks Skeleton to leave header-tools focus and restore prior chrome.
type ExitHeaderToolsMsg struct{}

// VaultStateMsg updates footer vault lock display (unidirectional Agent → Skeleton).
type VaultStateMsg struct {
	Mode   string
	Locked bool
	Known  bool
}
