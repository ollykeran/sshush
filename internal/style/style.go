package style

import (
	"fmt"
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
	highlight lipgloss.Style
	focus     lipgloss.Style
)

func rebuildStyles() {
	success = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(currentTheme.Accent))
	warn = lipgloss.NewStyle().Foreground(lipgloss.Color(currentTheme.Warning))
	err = lipgloss.NewStyle().Foreground(lipgloss.Color(currentTheme.Error))
	text = lipgloss.NewStyle().Foreground(lipgloss.Color(currentTheme.Text))
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

// Standalone style functions - all driven by the current theme (SetTheme).
func Success(s string) string   { return success.Render(s) }
func Text(s string) string      { return text.Render(s) }
func Highlight(s string) string { return highlight.Render(s) }
func Focus(s string) string     { return focus.Render(s) }
func Warn(s string) string      { return warn.Render(s) }
func Err(s string) string       { return err.Render(s) }
func Box(s string) string      { return box.Render(s) }

// Output is a builder for styled terminal output. Append lines with semantic
// level methods (Success/Info/Warn/Error), then flush with Print() or AsError().
type Output struct {
	lines []string
}

// NewOutput returns a new empty Output builder.
func NewOutput() *Output { return &Output{} }

// Semantic append methods - encode color from theme, callers describe intent.
func (o *Output) Success(s string) *Output { return o.add(success.Render(s)) }
func (o *Output) Info(s string) *Output    { return o.add(highlight.Render(s)) }
func (o *Output) Warn(s string) *Output   { return o.add(warn.Render(s)) }
func (o *Output) Error(s string) *Output  { return o.add(err.Render(s)) }

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

// Box renders all lines inside a rounded border box string.
func (o *Output) Box() string { return box.Render(strings.Join(o.lines, "\n")) }

// String renders all lines joined by newlines, without a border.
func (o *Output) String() string { return strings.Join(o.lines, "\n") }

// Print renders as a box to stdout.
func (o *Output) Print() {
	if o.Len() > 0 {
		fmt.Fprintln(os.Stdout, o.Box())
	}
}

// PrintTo renders as a box to w.
func (o *Output) PrintTo(w io.Writer) { fmt.Fprintln(w, o.Box()) }

// PrintErr renders as a box to stderr.
func (o *Output) PrintErr() {
	fmt.Fprintln(os.Stderr, o.Box())
}

// AsError wraps the Output in a StyledError for display at the Execute level.
// Use instead of errors.New() for all user-facing command errors.
func (o *Output) AsError() error { return &StyledError{o} }

// StyledError carries a pre-styled Output to be printed by Execute.
// It prevents cobra from printing a plain "Error: ..." line.
type StyledError struct{ out *Output }

// Error implements the error interface.
func (e *StyledError) Error() string { return e.out.String() }

// PrintErr prints the styled output to stderr.
func (e *StyledError) PrintErr() { e.out.PrintErr() }
