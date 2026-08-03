package tui

import tea "charm.land/bubbletea/v2"

// Shared key binding names for chrome and screens.
// Use KeyMatchesString(key, binding) or Matches(key, binding).

var (
	KeyUp        = []string{"up", "k"}
	KeyDown      = []string{"down", "j"}
	KeyLeft      = []string{"left", "h"}
	KeyRight     = []string{"right", "l"}
	KeyEnter     = []string{"enter"}
	KeyEsc       = []string{"esc", "escape"}
	KeyQuit      = []string{"q", "esc"}
	KeyTab       = []string{"tab"}
	KeyShiftTab  = []string{"shift+tab"}
	KeyHelp      = []string{"?"}
	KeyTheme     = []string{"t"}
	KeyBackspace = []string{"backspace", "delete"}

	// Agent global / daemon bindings.
	KeyDaemonFocus  = []string{"d"}
	KeyDaemonStart  = []string{"s"}
	KeyDaemonStop   = []string{"x"}
	KeyDaemonReload = []string{"r"}
	KeyDaemonLock   = []string{"L"}
	KeyDaemonUnlock = []string{"u"}
	KeyAddKey       = []string{"a"}

	// Aliases used by screens.
	KeyStart  = KeyDaemonStart
	KeyStop   = KeyDaemonStop
	KeyReload = KeyDaemonReload
	KeyLock   = KeyDaemonLock
	KeyUnlock = KeyDaemonUnlock
)

// Matches reports whether key equals any binding in keys.
func Matches(key string, keys []string) bool {
	for _, k := range keys {
		if key == k {
			return true
		}
	}
	return false
}

// KeyMatchesString is an alias for Matches (preferred name in chrome code).
func KeyMatchesString(key string, keys []string) bool {
	return Matches(key, keys)
}

// KeyMatches reports whether a KeyPressMsg matches any binding in keys.
func KeyMatches(msg tea.KeyPressMsg, keys []string) bool {
	return Matches(msg.String(), keys)
}

// containsKey is kept for SkeletonKeyMap slice checks.
func containsKey(keys []string, key string) bool {
	return Matches(key, keys)
}
