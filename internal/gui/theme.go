package gui

import (
	"encoding/hex"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	fynetheme "fyne.io/fyne/v2/theme"

	sshushtheme "github.com/ollykeran/sshush/internal/theme"
)

// sshushFyneTheme maps sshush semantic colours onto Fyne's theme hooks.
// Fyne reads many roles (background, button, input, …); only overriding a few
// left most chrome on the built-in theme. We derive surfaces from the five
// sshush roles so the GUI visibly matches TUI presets (dark vs light from text luminance).
// UIScale limits for theme.Size scaling (text, padding, controls). Independent of OS DPI.
const (
	minUIScale float32 = 0.85
	maxUIScale float32 = 1.60
)

type sshushFyneTheme struct {
	base fyne.Theme
	th   sshushtheme.Theme

	uiScale float32

	ok bool

	text, focus, accent, errC, warn color.NRGBA
	dark                            bool
	bg, bgElev                      color.NRGBA
	button                          color.NRGBA
	inputBG, inputBorder            color.NRGBA
	hover                           color.NRGBA
	selection                       color.NRGBA
	separator                       color.NRGBA
	scrollBar                       color.NRGBA
	disabledFG                      color.NRGBA
	disabledButton                  color.NRGBA
	placeholder                     color.NRGBA
}

// NewFyneTheme wraps the default Fyne theme and applies sshush palette across Fyne color roles.
// Text and spacing use the default scale (1.0); use NewFyneThemeScaled for GUI zoom.
func NewFyneTheme(th sshushtheme.Theme) fyne.Theme {
	return NewFyneThemeScaled(th, 1.0)
}

// NewFyneThemeScaled is like NewFyneTheme but scales theme sizes (text, padding, inputs) for readability.
func NewFyneThemeScaled(th sshushtheme.Theme, uiScale float32) fyne.Theme {
	uiScale = ClampUIScale(uiScale)
	t := &sshushFyneTheme{base: fynetheme.DefaultTheme(), th: th, uiScale: uiScale}
	t.initDerived()
	return t
}

// ClampUIScale snaps GUI size scale to the supported range (zoom in/out controls).
func ClampUIScale(s float32) float32 {
	if s < minUIScale {
		return minUIScale
	}
	if s > maxUIScale {
		return maxUIScale
	}
	return s
}

func (t *sshushFyneTheme) initDerived() {
	var ok bool
	t.text, ok = parseHex(t.th.Text)
	if !ok {
		return
	}
	t.focus, ok = parseHex(t.th.Focus)
	if !ok {
		return
	}
	t.accent, ok = parseHex(t.th.Accent)
	if !ok {
		return
	}
	t.errC, ok = parseHex(t.th.Error)
	if !ok {
		return
	}
	t.warn, ok = parseHex(t.th.Warning)
	if !ok {
		return
	}
	t.ok = true

	t.dark = luminance(t.text) > 0.45

	neutralDark := color.NRGBA{R: 0x1a, G: 0x1a, B: 0x1f, A: 0xff}
	neutralLight := color.NRGBA{R: 0xf4, G: 0xf4, B: 0xf7, A: 0xff}

	if t.dark {
		t.bg = blendNRGBA(neutralDark, t.accent, 0.18)
		t.bgElev = blendNRGBA(t.bg, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, 0.07)
		t.button = blendNRGBA(t.bg, t.accent, 0.42)
		t.inputBG = t.bgElev
		t.inputBorder = blendNRGBA(t.bg, t.focus, 0.5)
		t.hover = blendNRGBA(t.focus, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, 0.28)
		t.selection = blendNRGBA(t.bg, t.focus, 0.38)
		t.separator = blendNRGBA(t.bg, t.text, 0.22)
		t.scrollBar = blendNRGBA(t.bg, t.accent, 0.35)
		t.disabledFG = blendNRGBA(t.bg, t.text, 0.45)
		t.disabledButton = blendNRGBA(t.bg, t.button, 0.55)
		t.placeholder = blendNRGBA(t.bg, t.text, 0.35)
	} else {
		t.bg = blendNRGBA(neutralLight, t.accent, 0.06)
		t.bgElev = blendNRGBA(t.bg, color.NRGBA{R: 0, G: 0, B: 0, A: 0xff}, 0.04)
		t.button = blendNRGBA(t.bg, t.accent, 0.28)
		t.inputBG = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
		t.inputBorder = blendNRGBA(t.inputBG, t.focus, 0.35)
		t.hover = blendNRGBA(t.focus, color.NRGBA{R: 0, G: 0, B: 0, A: 0xff}, 0.12)
		t.selection = blendNRGBA(t.bg, t.focus, 0.22)
		t.separator = blendNRGBA(t.bg, t.text, 0.18)
		t.scrollBar = blendNRGBA(t.bg, t.accent, 0.25)
		t.disabledFG = blendNRGBA(t.bg, t.text, 0.5)
		t.disabledButton = blendNRGBA(t.bg, t.button, 0.45)
		t.placeholder = blendNRGBA(t.bg, t.text, 0.42)
	}
}

func (t *sshushFyneTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if !t.ok {
		return t.base.Color(n, v)
	}

	switch n {
	case fynetheme.ColorNameBackground:
		return t.bg
	case fynetheme.ColorNameForeground:
		return t.text
	case fynetheme.ColorNamePrimary:
		return t.focus
	case fynetheme.ColorNameFocus:
		return t.focus
	case fynetheme.ColorNameSuccess:
		return t.accent
	case fynetheme.ColorNameError:
		return t.errC
	case fynetheme.ColorNameWarning:
		return t.warn
	case fynetheme.ColorNameButton:
		return t.button
	case fynetheme.ColorNameDisabledButton:
		return t.disabledButton
	case fynetheme.ColorNameDisabled:
		return t.disabledFG
	case fynetheme.ColorNameInputBackground:
		return t.inputBG
	case fynetheme.ColorNameInputBorder:
		return t.inputBorder
	case fynetheme.ColorNameHover:
		return t.hover
	case fynetheme.ColorNameHyperlink:
		return t.accent
	case fynetheme.ColorNamePlaceHolder:
		return t.placeholder
	case fynetheme.ColorNameSelection:
		return t.selection
	case fynetheme.ColorNameSeparator:
		return t.separator
	case fynetheme.ColorNameScrollBar:
		return t.scrollBar
	case fynetheme.ColorNameScrollBarBackground:
		return t.bgElev
	case fynetheme.ColorNameHeaderBackground:
		return t.bgElev
	case fynetheme.ColorNameMenuBackground:
		return t.bgElev
	case fynetheme.ColorNameOverlayBackground:
		return t.bgElev
	case fynetheme.ColorNameForegroundOnPrimary:
		return contrastingOn(t.focus)
	case fynetheme.ColorNameForegroundOnSuccess:
		return contrastingOn(t.accent)
	case fynetheme.ColorNameForegroundOnError:
		return contrastingOn(t.errC)
	case fynetheme.ColorNameForegroundOnWarning:
		return contrastingOn(t.warn)
	case fynetheme.ColorNamePressed:
		if t.dark {
			return blendNRGBA(t.focus, color.NRGBA{R: 0, G: 0, B: 0, A: 0xff}, 0.22)
		}
		return blendNRGBA(t.focus, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, 0.15)
	case fynetheme.ColorNameShadow:
		if t.dark {
			return color.NRGBA{R: 0, G: 0, B: 0, A: 0x90}
		}
		return color.NRGBA{R: 0, G: 0, B: 0, A: 0x45}
	default:
		return t.base.Color(n, v)
	}
}

func (t *sshushFyneTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.base.Font(style)
}

func (t *sshushFyneTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

func (t *sshushFyneTheme) Size(name fyne.ThemeSizeName) float32 {
	v := t.base.Size(name)
	if t.uiScale <= 0 {
		return v
	}
	return v * t.uiScale
}

func luminance(c color.NRGBA) float64 {
	r := float64(c.R) / 255
	g := float64(c.G) / 255
	b := float64(c.B) / 255
	return 0.299*r + 0.587*g + 0.114*b
}

func blendNRGBA(a, b color.NRGBA, t float64) color.NRGBA {
	return color.NRGBA{
		R: uint8(float64(a.R)*(1-t) + float64(b.R)*t),
		G: uint8(float64(a.G)*(1-t) + float64(b.G)*t),
		B: uint8(float64(a.B)*(1-t) + float64(b.B)*t),
		A: 0xff,
	}
}

func contrastingOn(c color.NRGBA) color.NRGBA {
	if luminance(c) > 0.5 {
		return color.NRGBA{R: 0x12, G: 0x12, B: 0x14, A: 0xff}
	}
	return color.NRGBA{R: 0xf8, G: 0xf8, B: 0xfa, A: 0xff}
}

func hexOrDefault(hexStr string, fallback color.Color) color.Color {
	c, ok := parseHex(hexStr)
	if !ok {
		return fallback
	}
	return c
}

func parseHex(s string) (color.NRGBA, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "#") || len(s) != 7 {
		return color.NRGBA{}, false
	}
	if !sshushtheme.ValidHex(s) {
		return color.NRGBA{}, false
	}
	b, err := hex.DecodeString(s[1:])
	if err != nil || len(b) != 3 {
		return color.NRGBA{}, false
	}
	return color.NRGBA{R: b[0], G: b[1], B: b[2], A: 0xff}, true
}
