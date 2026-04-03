package style

import (
	"os"

	"golang.org/x/term"
)

// TerminalColumns returns the width in columns of the tty attached to stderr,
// or if stderr is not a tty, stdout. Returns 0 if neither is a terminal or size
// cannot be read.
func TerminalColumns() int {
	for _, f := range []*os.File{os.Stderr, os.Stdout} {
		fd := int(f.Fd())
		if term.IsTerminal(fd) {
			w, _, err := term.GetSize(fd)
			if err == nil && w > 0 {
				return w
			}
		}
	}
	return 0
}

// effectiveBoxLimit returns the maximum outer width (columns) for a boxed block:
// terminal width when a tty is available, optionally capped by maxCap when maxCap > 0.
// With no tty and maxCap <= 0, returns 0 (no limit; natural width).
func effectiveBoxLimit(maxCap int) int {
	cols := TerminalColumns()
	if cols > 0 {
		w := cols
		if maxCap > 0 && maxCap < w {
			w = maxCap
		}
		return w
	}
	if maxCap > 0 {
		return maxCap
	}
	return 0
}
