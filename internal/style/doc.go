// Package style provides styled terminal output using Lipgloss.
// Output builds lines with Success/Info/Warn/Error and prints as a box.
// When stderr or stdout is a tty, boxed output wraps only if content is wider
// than the terminal; otherwise the box stays as narrow as the content.
package style
