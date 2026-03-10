package theme

import "strings"

// Presets maps preset name (lowercase) to theme. Used for case-insensitive lookup.
var Presets = map[string]Theme{
	"default": defaultTheme,

	"dracula": {
		Text:    "#F8F8F2",
		Focus:   "#50FA7B",
		Accent:  "#BD93F9",
		Error:   "#FF5555",
		Warning: "#F1FA8C",
	},

	"nord": {
		Text:    "#d8dee9",
		Focus:   "#a3be8c",
		Accent:  "#88c0d0",
		Error:   "#bf616a",
		Warning: "#ebcb8b",
	},

	"solarized-dark": {
		Text:    "#839496",
		Focus:   "#859900",
		Accent:  "#268bd2",
		Error:   "#dc322f",
		Warning: "#b58900",
	},

	"catppuccin-latte": {
		Text:    "#4c4f69",
		Focus:   "#40a02b",
		Accent:  "#8839ef",
		Error:   "#d20f39",
		Warning: "#df8e1d",
	},

	"catppuccin-mocha": {
		Text:    "#cdd6f4",
		Focus:   "#a6e3a1",
		Accent:  "#cba6f7",
		Error:   "#f38ba8",
		Warning: "#f9e2af",
	},
}

// PresetNames returns the list of built-in preset names (map order, non-deterministic).
func PresetNames() []string {
	names := make([]string, 0, len(Presets))
	for k := range Presets {
		names = append(names, k)
	}
	return names
}

// PresetNamesOrdered returns preset names in a stable display order.
func PresetNamesOrdered() []string {
	return []string{
		"default", "dracula", "nord", "solarized-dark",
		"catppuccin-latte", "catppuccin-frappe", "catppuccin-macchiato", "catppuccin-mocha",
	}
}

// ResolveTheme returns the theme for the given preset name. Lookup is case-insensitive.
// Returns (theme, true) if found; otherwise (DefaultTheme(), false).
func ResolveTheme(presetName string) (Theme, bool) {
	key := strings.ToLower(strings.TrimSpace(presetName))
	t, ok := Presets[key]
	if !ok {
		return DefaultTheme(), false
	}
	return t, true
}
