package tui

import zone "github.com/lrstanley/bubblezone"

func inZoneBounds(id string, x, y int) bool {
	z := zone.Get(id)
	if z == nil {
		return false
	}
	return x >= z.StartX && x <= z.EndX && y >= z.StartY && y <= z.EndY
}

// sectionBoxCursorPos returns a text cursor position from a click inside a
// zone-marked SectionBox. Returns -1 if the click is not on the content line
// (title=Y+0, border-top=Y+1, content=Y+2, border-bottom=Y+3).
func sectionBoxCursorPos(id string, x, y int) int {
	z := zone.Get(id)
	if z == nil {
		return -1
	}
	contentY := z.StartY + 2
	if y != contentY {
		return -1
	}
	pos := x - z.StartX - 2
	if pos < 0 {
		return 0
	}
	return pos
}
