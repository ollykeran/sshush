package tray

// The main window lays itself out in code, not from its template, so it can
// be resized: the template only creates the controls. mainLayout places
// every control for a client area and the controls' natural widths, and is
// plain arithmetic, so it is tested on every OS.
//
// The window follows the list/details pattern: the keys on the left, and on
// the right everything about the one selected, with what can be done to it.
// Above both, a header naming the agent with the buttons that change its
// state, and a command bar for what is not about one key; below, a status
// line and a footer of settings.

// box is a control's rectangle in client pixels.
type box struct{ x, y, w, h int }

// layoutMetrics is what mainLayout needs to know beyond the client size.
type layoutMetrics struct {
	dpi int
	// natural is the width each button or label needs for its content, in
	// pixels at dpi. A control missing from it keeps a fixed width.
	natural map[uint16]int
	// pubLines is how many lines the public key wraps to at the details
	// pane's width; 0 when nothing is selected.
	pubLines int
	// hidden holds the buttons not shown now, which give up their place in
	// a row to the next.
	hidden map[uint16]bool
}

// Sizes at 96 DPI, after Windows' own: 32-pixel buttons, 16-pixel margins.
const (
	lyMargin     = 16
	lyGap        = 8
	lyButtonH    = 32
	lyFieldH     = 26
	lyLabelH     = 18
	lyStateIcon  = 32
	lyTitleH     = 28
	lyDetailH    = 24
	lyListMinW   = 280
	lyListMaxW   = 460
	lyDetailMinW = 340
	lyLineH      = 18 // one line of the public key
	lyPubMinLine = 2
	lyPubMaxLine = 6
	lyComboW     = 132
	lyBodyMinH   = 340
)

// detailButtons are the selected key's actions, in reading order. Remove is
// apart from the rest, at the far end, so it is not pressed for its neighbour.
var detailButtons = []uint16{idMainLoad, idMainAutoload, idMainRename}

// scale converts px at 96 DPI to dpi, rounding to the nearest pixel.
func scale(px, dpi int) int { return (px*dpi + 48) / 96 }

// mainMinSize is the smallest client area mainLayout fits everything in.
func mainMinSize(m layoutMetrics) (w, h int) {
	s := func(v int) int { return scale(v, m.dpi) }
	w = s(lyMargin)*2 + s(lyListMinW) + s(lyMargin) + s(lyDetailMinW)
	h = s(lyMargin) + s(lyTitleH+lyLabelH) + s(lyMargin) + s(lyButtonH) + s(lyGap) +
		s(lyBodyMinH) + s(lyGap) + s(lyLabelH) + s(lyGap) + s(lyButtonH) + s(lyMargin)
	return w, h
}

// mainLayout places every control of the main window in a cw by ch client area.
func mainLayout(cw, ch int, m layoutMetrics) map[uint16]box {
	s := func(v int) int { return scale(v, m.dpi) }
	nat := func(id uint16, fallback int) int {
		if w, ok := m.natural[id]; ok {
			return w
		}
		return s(fallback)
	}
	out := map[uint16]box{}
	margin, gap, btnH := s(lyMargin), s(lyGap), s(lyButtonH)
	right := cw - margin

	// Header: the state icon, the agent's name and where its keys are, and
	// at the right the buttons that change its state.
	out[idMainStateIcon] = box{margin, margin + s(2), s(lyStateIcon), s(lyStateIcon)}
	x := right
	for _, id := range []uint16{idMainAgentToggle, idMainLock} {
		if m.hidden[id] {
			continue
		}
		w := nat(id, 100)
		x -= w
		out[id] = box{x, margin + s(4), w, btnH}
		x -= gap
	}
	textX := margin + s(lyStateIcon) + s(12)
	out[idMainAgent] = box{textX, margin - s(2), max(x-textX, 0), s(lyTitleH)}
	out[idMainSource] = box{textX, margin + s(lyTitleH) - s(2), max(x-textX, 0), s(lyLabelH)}

	// Command bar: what is not about one key.
	barY := margin + s(lyTitleH+lyLabelH) + s(lyMargin)
	x = margin
	for _, id := range []uint16{idMainAdd, idMainCreateVault, idMainCreateKey} {
		if m.hidden[id] {
			continue
		}
		w := nat(id, 90)
		out[id] = box{x, barY, w, btnH}
		x += w + gap
	}
	w := nat(idMainRefresh, 90)
	out[idMainRefresh] = box{right - w, barY, w, btnH}

	// Footer and the status line above it.
	footY := ch - margin - btnH
	statusY := footY - gap - s(lyLabelH)
	x = margin
	out[idMainSelftest] = box{x, footY, nat(idMainSelftest, 110), btnH}
	x += out[idMainSelftest].w + s(lyMargin)
	lw := nat(idMainThemeLabel, 44)
	out[idMainThemeLabel] = box{x, footY + (btnH-s(lyLabelH))/2, lw, s(lyLabelH)}
	x += lw + gap
	// A drop-down list's height includes its open list.
	out[idMainTheme] = box{x, footY + (btnH-s(lyFieldH))/2, s(lyComboW), s(160)}
	x += s(lyComboW) + s(lyMargin)
	out[idMainStartup] = box{x, footY + (btnH-s(lyFieldH))/2, nat(idMainStartup, 140), s(lyFieldH)}
	// The version is the least of the footer: it shortens, then goes, to
	// make room for the settings.
	afterStartup := out[idMainStartup].x + out[idMainStartup].w + s(lyMargin)
	vw := max(min(nat(idMainVersion, 80), right-afterStartup), 0)
	out[idMainVersion] = box{right - vw, footY + (btnH-s(lyLabelH))/2, vw, s(lyLabelH)}
	out[idMainStatus] = box{margin, statusY, right - margin, s(lyLabelH)}

	// Body: the list, and beside it the details pane.
	bodyY := barY + btnH + gap
	bodyH := max(statusY-gap-bodyY, 0)
	inner := right - margin
	listW := min(max((inner-s(lyMargin))*5/12, s(lyListMinW)), s(lyListMaxW))
	if inner-listW-s(lyMargin) < s(lyDetailMinW) {
		listW = max(inner-s(lyMargin)-s(lyDetailMinW), s(lyListMinW))
	}
	out[idMainList] = box{margin, bodyY, listW, bodyH}

	dx := margin + listW + s(lyMargin)
	dw := max(right-dx, 0)
	y := bodyY
	out[idMainDetailTitle] = box{dx, y, dw, s(lyDetailH)}
	y += s(lyDetailH)
	out[idMainDetailSub] = box{dx, y, dw, s(lyLabelH)}
	y += s(lyLabelH) + s(lyMargin)

	// A label over a read-only field, with its Copy button beside the field.
	field := func(label, edit, copyBtn uint16, h int) {
		out[label] = box{dx, y, dw, s(lyLabelH)}
		y += s(lyLabelH) + s(4)
		cw := nat(copyBtn, 72)
		by := y // level with the top of a tall field, centred on a short one
		if h < btnH {
			by = y + (h-btnH)/2
		}
		out[copyBtn] = box{dx + dw - cw, by, cw, btnH}
		out[edit] = box{dx, y, max(dw-cw-gap, 0), h}
		y += max(h, btnH) + s(12)
	}
	// The public key's field is as tall as the key wraps to, up to a limit
	// and as room allows; past that it scrolls.
	rows := detailButtonRows(dw, gap, nat, m.hidden)
	fixed := (y - bodyY) + 2*(s(lyLabelH)+s(4)+s(12)) + max(s(lyFieldH), btnH) + s(8) +
		s(lyLabelH) + s(4) + s(lyLabelH) + s(lyMargin) + rows*btnH + (rows-1)*gap
	room := (bodyH - fixed) / s(lyLineH)
	lines := max(min(m.pubLines, lyPubMaxLine, room), lyPubMinLine)

	field(idMainFpLabel, idMainFp, idMainCopyFp, s(lyFieldH))
	field(idMainPubLabel, idMainPub, idMainCopyPub, s(lyLineH)*lines+s(8))
	out[idMainFromLabel] = box{dx, y, dw, s(lyLabelH)}
	y += s(lyLabelH) + s(4)
	out[idMainFrom] = box{dx, y, dw, s(lyLabelH)}
	y += s(lyLabelH) + s(lyMargin)

	// The key's actions flow along a row, wrapping when the pane is narrow;
	// Remove ends the last row, at the right.
	x = dx
	for _, id := range detailButtons {
		if m.hidden[id] {
			continue
		}
		w := nat(id, 90)
		if x > dx && x+w > dx+dw {
			x, y = dx, y+btnH+gap
		}
		out[id] = box{x, y, w, btnH}
		x += w + gap
	}
	rw := nat(idMainRemove, 90)
	if x+rw > dx+dw {
		y += btnH + gap
	}
	out[idMainRemove] = box{dx + dw - rw, y, rw, btnH}
	return out
}

// detailButtonRows is how many rows the key's actions take in a pane dw wide.
func detailButtonRows(dw, gap int, nat func(uint16, int) int, hidden map[uint16]bool) int {
	rows, x := 1, 0
	for _, id := range detailButtons {
		if hidden[id] {
			continue
		}
		w := nat(id, 90)
		if x > 0 && x+w > dw {
			rows, x = rows+1, 0
		}
		x += w + gap
	}
	if x+nat(idMainRemove, 90) > dw {
		rows++
	}
	return rows
}
