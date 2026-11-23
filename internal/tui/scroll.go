package tui

import "fmt"

// scrollOffset returns the first row to draw for a list of total rows shown
// height rows at a time, so that cursor stays in view. It moves the previous
// offset only as far as needed, so the window does not jump while the cursor
// moves inside it.
func scrollOffset(offset, cursor, total, height int) int {
	if height <= 0 || total <= height {
		return 0
	}
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+height {
		offset = cursor - height + 1
	}
	if maxOff := total - height; offset > maxOff {
		offset = maxOff
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

// scrollHint describes which rows of a scrolled list are on screen, e.g.
// "↑↓ 3–8 of 14". The arrows show which way there is more to see.
func scrollHint(offset, shown, total int) string {
	arrows := ""
	if offset > 0 {
		arrows += "↑"
	}
	if offset+shown < total {
		arrows += "↓"
	}
	return fmt.Sprintf("%s %d–%d of %d", arrows, offset+1, offset+shown, total)
}
