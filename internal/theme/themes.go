package theme

import (
	"sort"
	"strings"
)

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

	"3024-day": {
		Text:    "#4a4543",
		Focus:   "#01a252",
		Accent:  "#01a0e4",
		Error:   "#db2d20",
		Warning: "#fded02",
	},
	"3024-night": {
		Text:    "#a5a2a2",
		Focus:   "#01a252",
		Accent:  "#01a0e4",
		Error:   "#db2d20",
		Warning: "#fded02",
	},
	"aurelia-retro": {
		Text:    "#EA549F",
		Focus:   "#4EC9B0",
		Accent:  "#579BD5",
		Error:   "#E92888",
		Warning: "#CE9178",
	},
	"charmquark": {
		Text:    "#FFFFFF",
		Focus:   "#A6E22E",
		Accent:  "#AE81FF",
		Error:   "#F92672",
		Warning: "#66D9EF",
	},
	"cobalt2": {
		Text:    "#c7c7c7",
		Focus:   "#3AD900",
		Accent:  "#1478DB",
		Error:   "#ff2600",
		Warning: "#ffc600",
	},
	"cyberpunk": {
		Text:    "#e5e5e5",
		Focus:   "#00fbac",
		Accent:  "#00bfff",
		Error:   "#ff7092",
		Warning: "#fffa6a",
	},
	"monokai-night": {
		Text:    "#f8f8f8",
		Focus:   "#a6e22e",
		Accent:  "#6699df",
		Error:   "#f92672",
		Warning: "#e6db74",
	},
	"monokai-set": {
		Text:    "#F8F8F2",
		Focus:   "#98F424",
		Accent:  "#9D65FF",
		Error:   "#F4005F",
		Warning: "#FA8419",
	},
	"night-owl": {
		Text:    "#D6DEEB",
		Focus:   "#22DA6E",
		Accent:  "#82AAFF",
		Error:   "#EF5350",
		Warning: "#ADDB67",
	},
	"one-half-dark": {
		Text:    "#DCDFE4",
		Focus:   "#98C379",
		Accent:  "#C678DD",
		Error:   "#E06C75",
		Warning: "#E5C07B",
	},
	"polygone": {
		Text:    "#ffffff",
		Focus:   "#2fff24",
		Accent:  "#6272a4",
		Error:   "#ff2424",
		Warning: "#ffc400",
	},
	"putty": {
		Text:    "#bbbbbb",
		Focus:   "#64d238",
		Accent:  "#3442f1",
		Error:   "#ff5555",
		Warning: "#ffff55",
	},
	"smyck": {
		Text:    "#F8F8F8",
		Focus:   "#8EB33B",
		Accent:  "#4E90A7",
		Error:   "#C75646",
		Warning: "#D0B03C",
	},
	"solarized-dark-patched": {
		Text:    "#fdf6e3",
		Focus:   "#859900",
		Accent:  "#268bd2",
		Error:   "#dc322f",
		Warning: "#b58900",
	},
	"synthwave": {
		Text:    "#dad9c7",
		Focus:   "#1ebb2b",
		Accent:  "#2186ec",
		Error:   "#f6188f",
		Warning: "#fdf834",
	},
	"thanatos-dark": {
		Text:    "#e09887",
		Focus:   "#0de1b1",
		Accent:  "#0e9bd1",
		Error:   "#ce4559",
		Warning: "#d8cb32",
	},
	"void-scheme": {
		Text:    "#d0d2df",
		Focus:   "#a6fa62",
		Accent:  "#7f7ab6",
		Error:   "#be6482",
		Warning: "#ad8f5d",
	},
	"vscode": {
		Text:    "#D3D3D3",
		Focus:   "#3FC48A",
		Accent:  "#579BD5",
		Error:   "#D8473F",
		Warning: "#D7BA7D",
	},
	"x-dotshare": {
		Text:    "#D7D0C7",
		Focus:   "#B8D68C",
		Accent:  "#7DC1CF",
		Error:   "#E84F4F",
		Warning: "#E1AA5D",
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

// PresetNamesOrdered returns preset names in stable alphabetical order.
func PresetNamesOrdered() []string {
	names := PresetNames()
	sort.Strings(names)
	return names
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
