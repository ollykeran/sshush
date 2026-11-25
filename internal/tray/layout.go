package tray

// The main window lays itself out in code, not from its template, so it can
// be resized: the template only creates the controls. mainLayout places
// every control for a client area and the controls' natural widths, and is
// plain arithmetic, so it is tested on every OS.
//
// Under a header naming the agent, with the button that starts or stops it,
// a tab strip switches the body between two pages. The Keys page follows the
// list/details pattern: a command bar for what is not about one key, the keys
// on the left, and on the right everything about the one selected, with what
// can be done to it. The Vault page is about the vault itself: its state, a
// column of facts, and its actions. Below both, a status line and a footer
// of settings. Both pages are always placed; the window shows one.

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
	lyRowH       = 24 // one of the Vault page's facts
	lyRowLabelW  = 120
)

// vaultButtons are the Vault page's actions, in reading order. Create vault
// and Lock share a place: a vault that does not exist yet cannot be locked.
var vaultButtons = []uint16{idMainCreateVault, idMainLock, idVaultPasswd, idVaultRecovery, idVaultBackup, idVaultImport, idVaultOpenFolder, idVaultCopyPath, idVaultMode}

// vaultPage holds every control on the Vault page.
var vaultPage = func() map[uint16]bool {
	m := map[uint16]bool{idVaultTitle: true, idVaultSub: true}
	for _, id := range vaultButtons {
		m[id] = true
	}
	for i := range vaultRowCount {
		m[vaultRowLabel(i)], m[vaultRowValue(i)] = true, true
	}
	return m
}()

// detailButtons are the selected key's actions, in reading order. Remove is
// apart from the rest, at the far end, so it is not pressed for its neighbour.
var detailButtons = []uint16{idMainLoad, idMainAutoload, idMainRename}

// scale converts px at 96 DPI to dpi, rounding to the nearest pixel.
func scale(px, dpi int) int { return (px*dpi + 48) / 96 }

// mainMinSize is the smallest client area mainLayout fits everything in.
func mainMinSize(m layoutMetrics) (w, h int) {
	s := func(v int) int { return scale(v, m.dpi) }
	w = s(lyMargin)*2 + s(lyListMinW) + s(lyMargin) + s(lyDetailMinW)
	h = s(lyMargin) + s(lyTitleH+lyLabelH) + s(lyGap) + s(lyButtonH) + s(lyGap) + s(lyButtonH) + s(lyGap) +
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
	// at the right the button that starts or stops it.
	out[idMainStateIcon] = box{margin, margin + s(2), s(lyStateIcon), s(lyStateIcon)}
	x := right
	if !m.hidden[idMainAgentToggle] {
		w := nat(idMainAgentToggle, 100)
		x -= w
		out[idMainAgentToggle] = box{x, margin + s(4), w, btnH}
		x -= gap
	}
	textX := margin + s(lyStateIcon) + s(12)
	out[idMainAgent] = box{textX, margin - s(2), max(x-textX, 0), s(lyTitleH)}
	out[idMainSource] = box{textX, margin + s(lyTitleH) - s(2), max(x-textX, 0), s(lyLabelH)}

	// The page tabs, side by side, the same width.
	tabY := margin + s(lyTitleH+lyLabelH) + gap
	tw := max(nat(idMainTabKeys, 90), nat(idMainTabVault, 90))
	out[idMainTabKeys] = box{margin, tabY, tw, btnH}
	out[idMainTabVault] = box{margin + tw, tabY, tw, btnH}

	// Command bar: what is not about one key.
	barY := tabY + btnH + gap
	x = margin
	for _, id := range []uint16{idMainAdd, idMainCreateKey} {
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

	vaultLayout(out, box{margin, barY, inner, max(statusY-gap-barY, 0)}, m)
	return out
}

// vaultLayout places the Vault page in body: its title and what it means,
// the facts as label and value, and the actions in a row that wraps.
func vaultLayout(out map[uint16]box, body box, m layoutMetrics) {
	s := func(v int) int { return scale(v, m.dpi) }
	nat := func(id uint16, fallback int) int {
		if w, ok := m.natural[id]; ok {
			return w
		}
		return s(fallback)
	}
	gap, btnH := s(lyGap), s(lyButtonH)
	y := body.y
	out[idVaultTitle] = box{body.x, y, body.w, s(lyDetailH)}
	y += s(lyDetailH)
	out[idVaultSub] = box{body.x, y, body.w, 2 * s(lyLabelH)} // it may wrap once
	y += 2*s(lyLabelH) + s(lyMargin)

	lw := min(s(lyRowLabelW), body.w/3)
	for i := range vaultRowCount {
		out[vaultRowLabel(i)] = box{body.x, y, lw, s(lyLabelH)}
		out[vaultRowValue(i)] = box{body.x + lw + gap, y, max(body.w-lw-gap, 0), s(lyLabelH)}
		if !m.hidden[vaultRowLabel(i)] {
			y += s(lyRowH)
		}
	}
	y += s(lyMargin) - (s(lyRowH) - s(lyLabelH))

	x := body.x
	for _, id := range vaultButtons {
		if m.hidden[id] {
			continue
		}
		w := nat(id, 100)
		if x > body.x && x+w > body.x+body.w {
			x, y = body.x, y+btnH+gap
		}
		out[id] = box{x, y, w, btnH}
		x += w + gap
	}
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
