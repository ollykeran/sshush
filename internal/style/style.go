package style

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderer targets stderr so color detection works even when stdout is piped (e.g. eval $(sshush start)).
var renderer = lipgloss.NewRenderer(os.Stderr)

var (
	green  = renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("#7EE787"))
	pink   = renderer.NewStyle().Foreground(lipgloss.Color("#F472B6"))
	purple = renderer.NewStyle().Foreground(lipgloss.Color("#631596"))
	warn   = renderer.NewStyle().Foreground(lipgloss.Color("#FBBF24"))
	err    = renderer.NewStyle().Foreground(lipgloss.Color("#F87171"))
	box    = renderer.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#631596")).
		Padding(0, 1)
)

// Standalone style functions - used for pre-styled strings (e.g. diff prefix symbols).
func Green(s string) string  { return green.Render(s) }
func Pink(s string) string   { return pink.Render(s) }
func Purple(s string) string { return purple.Render(s) }
func Warn(s string) string   { return warn.Render(s) }
func Err(s string) string    { return err.Render(s) }
func Box(s string) string    { return box.Render(s) }

// Output is a builder for styled terminal output. Append lines with semantic
// level methods (Success/Info/Warn/Error), then flush with Print() or AsError().
type Output struct {
	lines []string
}

// NewOutput returns a new empty Output builder.
func NewOutput() *Output { return &Output{} }

// Semantic append methods - encode color, callers describe intent not appearance.
func (o *Output) Success(s string) *Output { return o.add(green.Render(s)) }
func (o *Output) Info(s string) *Output    { return o.add(pink.Render(s)) }
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
func (o *Output) PrintErr() { fmt.Fprintln(os.Stderr, o.Box()) }

// AsError wraps the Output in a StyledError for display at the Execute level.
// Use instead of errors.New() for all user-facing command errors.
func (o *Output) AsError() error { return &StyledError{o} }

// StyledError carries a pre-styled Output to be printed by Execute.
// It prevents cobra from printing a plain "Error: ..." line.
type StyledError struct{ out *Output }

func (e *StyledError) Error() string { return e.out.String() }
func (e *StyledError) PrintErr()     { e.out.PrintErr() }
