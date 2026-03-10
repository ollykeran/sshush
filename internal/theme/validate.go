package theme

import (
	"image/color"
	"regexp"
	"strconv"
)

// hexRegex matches # followed by exactly 6 hexadecimal digits.
var hexRegex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// ValidHex returns true if s is a valid hex colour (#RRGGBB).
func ValidHex(s string) bool {
	return hexRegex.MatchString(s)
}

// MergeWithDefault returns a new Theme where any empty or invalid hex in custom
// is replaced by the corresponding value from defaultTheme.
func MergeWithDefault(custom, defaultTheme Theme) Theme {
	out := defaultTheme
	if custom.Text != "" && ValidHex(custom.Text) {
		out.Text = custom.Text
	}
	if custom.Focus != "" && ValidHex(custom.Focus) {
		out.Focus = custom.Focus
	}
	if custom.Accent != "" && ValidHex(custom.Accent) {
		out.Accent = custom.Accent
	}
	if custom.Error != "" && ValidHex(custom.Error) {
		out.Error = custom.Error
	}
	if custom.Warning != "" && ValidHex(custom.Warning) {
		out.Warning = custom.Warning
	}
	return out
}

// HexToRGBA parses a hex colour string (#RRGGBB) and returns image/color.RGBA.
// Returns black with ok false on parse failure.
func HexToRGBA(hex string) (c color.RGBA, ok bool) {
	if !ValidHex(hex) {
		return color.RGBA{}, false
	}
	r, _ := strconv.ParseUint(hex[1:3], 16, 8)
	g, _ := strconv.ParseUint(hex[3:5], 16, 8)
	b, _ := strconv.ParseUint(hex[5:7], 16, 8)
	return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}, true
}
