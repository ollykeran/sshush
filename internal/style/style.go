package style

import (
	"fmt"
	"image/color"
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ollykeran/sshush/internal/theme"
)

var currentTheme theme.Theme

func init() {
	currentTheme = theme.DefaultTheme()
	rebuildStyles()
}

var (
	success   lipgloss.Style
	warn      lipgloss.Style
	err       lipgloss.Style
	box       lipgloss.Style
	text      lipgloss.Style
	textBold  lipgloss.Style
	highlight lipgloss.Style
	focus     lipgloss.Style
)

func rebuildStyles() {
	success = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(currentTheme.Accent))
	warn = lipgloss.NewStyle().Foreground(lipgloss.Color(currentTheme.Warning))
	err = lipgloss.NewStyle().Foreground(lipgloss.Color(currentTheme.Error))
	text = lipgloss.NewStyle().Foreground(lipgloss.Color(currentTheme.Text))
	textBold = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(currentTheme.Text))
	highlight = lipgloss.NewStyle().Foreground(lipgloss.Color(currentTheme.Accent))
	focus = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(currentTheme.Focus))
	box = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(currentTheme.Focus)).
		Padding(0, 1)
}

// SetTheme sets the theme used by all style functions and Output. Call after loading config (e.g. in root PersistentPreRunE).
func SetTheme(t theme.Theme) {
	currentTheme = t
	rebuildStyles()
}

// StylesForInput returns the box, focus, and blurred lipgloss styles for use by input components (e.g. passphrase prompt).
// Use focusStyle for focused state, blurredStyle for unfocused (e.g. placeholder/secondary text).
func StylesForInput() (boxStyle, focusStyle, blurredStyle lipgloss.Style) {
	return box, focus, text
}

// InputCursorColor returns the theme focus color for the text input cursor (e.g. Cursor.Color).
func InputCursorColor() color.Color {
	return lipgloss.Color(currentTheme.Focus)
}

// Standalone style functions - all driven by the current theme (SetTheme).
func Success(s string) string   { return success.Render(s) }
func Text(s string) string      { return text.Render(s) }
func Highlight(s string) string { return highlight.Render(s) }
func Focus(s string) string     { return focus.Render(s) }
func Warn(s string) string      { return warn.Render(s) }
func Err(s string) string       { return err.Render(s) }
func Box(s string) string {
	return renderBox(s, effectiveBoxLimit(0))
}

// BoxWithMaxWidth renders content in a box with word wrapping when lines would
// exceed the limit: min(terminal width, maxWidth) on a tty when maxWidth > 0,
// otherwise terminal width; without a tty, maxWidth when maxWidth > 0.
// When content fits, the box stays as narrow as the content (no full-width padding).
func BoxWithMaxWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return box.Render(s)
	}
	return renderBox(s, effectiveBoxLimit(maxWidth))
}

func maxContentLineWidth(s string) int {
	max := 0
	for _, line := range strings.Split(s, "\n") {
		if w := lipgloss.Width(line); w > max {
			max = w
		}
	}
	return max
}

// renderBox renders at natural width when the outer block fits within limit;
// otherwise uses box.Width(limit) so long lines wrap on narrow terminals.
func renderBox(s string, limit int) string {
	if limit <= 0 {
		return box.Render(s)
	}
	inner := maxContentLineWidth(s)
	outerMin := inner + box.GetHorizontalFrameSize()
	if outerMin <= limit {
		return box.Render(s)
	}
	return box.Width(limit).Render(s)
}

// Output is a builder for styled terminal output. Append lines with semantic
// level methods (Success/Info/Warn/Error), then flush with Print() or AsError().
type Output struct {
	lines []string
}

// NewOutput returns a new empty Output builder.
func NewOutput() *Output { return &Output{} }

// Semantic append methods - encode color from theme, callers describe intent.
func (o *Output) Success(s string) *Output { return o.add(success.Render(s)) }
func (o *Output) Info(s string) *Output    { return o.add(text.Render(s)) }
func (o *Output) InfoBold(s string) *Output { return o.add(textBold.Render(s)) }
func (o *Output) Warn(s string) *Output    { return o.add(warn.Render(s)) }
func (o *Output) Error(s string) *Output   { return o.add(err.Render(s)) }

// Spacer appends a blank line for visual separation.
func (o *Output) Spacer() *Output { return o.add("") }

// Add appends a pre-styled string (use for diff symbols or composed lines).
func (o *Output) Add(s string) *Output { return o.add(s) }

func (o *Output) add(s string) *Output {
	o.lines = append(o.lines, s)
	return o
}

// Len returns the number of lines added.
func (o *Output) Len() int { return len(o.lines) }

// Box renders all lines inside a rounded border box string. On a tty, lines
// wider than the terminal wrap inside the box; otherwise the box is only as wide
// as the content.
func (o *Output) Box() string {
	return renderBox(strings.Join(o.lines, "\n"), effectiveBoxLimit(0))
}

// String renders all lines joined by newlines, without a border.
func (o *Output) String() string { return strings.Join(o.lines, "\n") }

// Print renders as a box to stdout.
func (o *Output) Print() {
	if o.Len() > 0 {
		fmt.Fprintln(os.Stdout, o.Box())
	}
}

// PrintTo renders as a box to w.
func (o *Output) PrintTo(w io.Writer) {
	if o.Len() > 0 {
		fmt.Fprintln(w, o.Box())
	}
}

// PrintErr renders as a box to stderr.
func (o *Output) PrintErr() {
	if o.Len() > 0 {
		fmt.Fprintln(os.Stderr, o.Box())
	}
}

// AsError wraps the Output in a StyledError for display at the Execute level.
// Use instead of errors.New() for all user-facing command errors.
func (o *Output) AsError() error { return &StyledError{o} }

// HexWithBackground renders the hex string (e.g. " #RRGGBB ") with that colour as the terminal background
// and a contrasting foreground. Returns plain hex if invalid.
func HexWithBackground(hex string) string {
	if !theme.ValidHex(hex) {
		return hex
	}
	fg, ok := theme.ContrastForeground(hex)
	if !ok {
		return hex
	}
	return lipgloss.NewStyle().
		Background(lipgloss.Color(hex)).
		Foreground(lipgloss.Color(fg)).
		Render(" " + hex + " ")
}

// StyledError carries a pre-styled Output to be printed by Execute.
// It prevents cobra from printing a plain "Error: ..." line.
type StyledError struct{ out *Output }

// Error implements the error interface.
func (e *StyledError) Error() string { return e.out.String() }

// PrintErr prints the styled output to stderr.
func (e *StyledError) PrintErr() { e.out.PrintErr() }
