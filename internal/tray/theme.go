package tray

// ThemeChoice is the tray's light or dark setting. ThemeSystem follows
// Windows' own "app mode" setting, and changes when it does.
type ThemeChoice string

// The choices the Theme menu offers, in menu order.
const (
	ThemeSystem ThemeChoice = "system"
	ThemeLight  ThemeChoice = "light"
	ThemeDark   ThemeChoice = "dark"
)

// themeChoices lists the Theme menu's items.
var themeChoices = []struct {
	choice ThemeChoice
	label  string
}{
	{ThemeSystem, "Follow system"},
	{ThemeLight, "Light"},
	{ThemeDark, "Dark"},
}

// ParseThemeChoice reads a stored choice. Anything unrecognised, including
// nothing stored, follows the system.
func ParseThemeChoice(s string) ThemeChoice {
	switch c := ThemeChoice(s); c {
	case ThemeLight, ThemeDark:
		return c
	}
	return ThemeSystem
}

// Dark reports whether c draws dark, given whether Windows' apps are dark.
// High contrast always wins: its colours are the user's, and drawing over
// them with a dark palette would undo what they chose it for.
func (c ThemeChoice) Dark(systemDark, highContrast bool) bool {
	if highContrast {
		return false
	}
	switch c {
	case ThemeDark:
		return true
	case ThemeLight:
		return false
	}
	return systemDark
}

// colorRef is a Win32 COLORREF, 0x00BBGGRR.
type colorRef uint32

func rgb(r, g, b uint8) colorRef { return colorRef(r) | colorRef(g)<<8 | colorRef(b)<<16 }

// The dark palette, after Windows 11's own dark dialogs. The light theme
// leaves every colour but the error text to Windows.
var (
	darkWindow              = rgb(0x20, 0x20, 0x20) // dialog background
	darkField               = rgb(0x19, 0x19, 0x19) // list and edit background
	darkText                = rgb(0xFF, 0xFF, 0xFF)
	darkErrorText           = rgb(0xFF, 0x99, 0xA4)
	lightErrorText colorRef = 0x001C2BC4 // #c42b1c
)

// Status and secondary colours, after Windows 11's: each pair is the light
// theme's, then the dark theme's.
var (
	toneColors = map[Tone][2]colorRef{
		ToneGood: {rgb(0x0F, 0x7B, 0x0F), rgb(0x6C, 0xCB, 0x5F)},
		ToneWarn: {rgb(0x9D, 0x5D, 0x00), rgb(0xFC, 0xE1, 0x00)},
		ToneBad:  {rgb(0xC4, 0x2B, 0x1C), rgb(0xFF, 0x99, 0xA4)},
	}
	secondaryText = [2]colorRef{rgb(0x5F, 0x5F, 0x5F), rgb(0xA8, 0xA8, 0xA8)}
	dimText       = [2]colorRef{rgb(0x8A, 0x8A, 0x8A), rgb(0x78, 0x78, 0x78)}
	glyphText     = [2]colorRef{rgb(0x1A, 0x1A, 0x1A), rgb(0xFF, 0xFF, 0xFF)}
	glyphDisabled = [2]colorRef{rgb(0xA8, 0xA8, 0xA8), rgb(0x5C, 0x5C, 0x5C)}
)

// pick chooses a pair's light or dark colour.
func pick(pair [2]colorRef, dark bool) colorRef {
	if dark {
		return pair[1]
	}
	return pair[0]
}

// toneColor is the colour for t, and false for ToneNeutral, which is drawn
// in the ordinary text colour.
func toneColor(t Tone, dark bool) (colorRef, bool) {
	pair, ok := toneColors[t]
	return pick(pair, dark), ok
}

// glyphColor is the colour of a button's icon: tinted for the buttons whose
// meaning a colour carries, else the text colour, and grey when disabled.
func glyphColor(id uint16, glyph rune, enabled, dark bool) colorRef {
	if !enabled {
		return pick(glyphDisabled, dark)
	}
	switch {
	case id == idMainAgentToggle && glyph == agentGlyph(MainActions{Start: true}):
		c, _ := toneColor(ToneGood, dark)
		return c
	case id == idMainAgentToggle, id == idMainRemove:
		c, _ := toneColor(ToneBad, dark)
		return c
	}
	return pick(glyphText, dark)
}
