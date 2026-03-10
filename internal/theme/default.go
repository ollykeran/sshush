package theme

// Default theme: current sshush-like palette (green/pink/purple).
var defaultTheme = Theme{
	Text:    "#585858",
	Focus:   "#7EE787",
	Accent:  "#F472B6",
	Error:   "#F87171",
	Warning: "#F2E94E",
}

// DefaultTheme returns the built-in default theme.
func DefaultTheme() Theme {
	return defaultTheme
}
