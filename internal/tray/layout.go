package tray

// The main window lays itself out in code, not from its template, so it can
// be resized: the template only creates the controls. mainLayout places
// every control for a client area and the controls' natural widths, and is
// plain arithmetic, so it is tested on every OS.
//
// Under a header naming the agent, with the button that starts or stops it,
// a tab strip switches the body between three pages. The Keys page follows
// the list/details pattern: a command bar for what is not about one key, the
// keys on the left, and on the right everything about the one selected, with
// what can be done to it. The Vault page is laid out the same way: what the
// vault is on the left, and on the right what can be done to it, as settings
// cards in groups, after Windows' own Settings: a title and a line of
// explanation, and the control at the right, every card's button the same
// width. The Settings page edits config in cards too. Controls are greyed,
// not hidden, when they cannot be used, so nothing moves as the state
// changes. Below every page, a status line and a footer. Every page is always
// placed; the window shows one.

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
	lyBodyMinH   = 360
	lyRowH       = 24 // one of the Vault page's facts
	lyRowLabelW  = 120
	lyHeaderH    = 20  // a section's header on a card page
	lyCardH      = 38  // a card: a title over a secondary line
	lyCardBtnW   = 120 // the narrowest card button, after Fluent's
	lySectionGap = 12
)

// vaultCardButtons are the Vault page's card buttons, which share one width.
var vaultCardButtons = []uint16{idVaultPasswd, idVaultRecovery, idVaultBackup, idVaultImport}

// autoLockControls are the Vault page's Auto-lock settings.
var autoLockControls = []uint16{idAutoLockTitle, idAutoLockCaption, idAutoLockSession, idAutoLockSleep, idAutoLockIdleLabel, idAutoLockIdle}

// vaultPage holds every control on the Vault page.
var vaultPage = func() map[uint16]bool {
	m := map[uint16]bool{idVaultTitle: true, idVaultSub: true, idMainCreateVault: true, idVaultOpenFolder: true, idVaultCopyPath: true}
	for _, id := range autoLockControls {
		m[id] = true
	}
	for _, id := range vaultCardButtons {
		m[id] = true
	}
	for id := uint16(idVaultSecHeader); id <= idVaultFileCaption; id++ {
		m[id] = true
	}
	for i := range vaultRowCount {
		m[vaultRowLabel(i)], m[vaultRowValue(i)] = true, true
	}
	return m
}()

// settingsPage holds every control on the Settings page: its own, and the
// tray's theme and start-up settings.
var settingsPage = func() map[uint16]bool {
	m := map[uint16]bool{idMainThemeLabel: true, idMainTheme: true, idMainStartup: true}
	for id := uint16(idSetAgentHeader); id <= idSetDiscard; id++ {
		m[id] = true
	}
	return m
}()

// sectionHeaders head the groups of cards, in the section font.
var sectionHeaders = []uint16{idVaultSecHeader, idAutoLockTitle, idVaultFileHeader,
	idSetAgentHeader, idSetVaultHeader, idSetTrayHeader, idSetKeysHeader, idSetDirsHeader}

// secondaryLabels are drawn in the secondary text colour: the cards'
// explanations.
var secondaryLabels = map[uint16]bool{
	idVaultPasswdCaption: true, idVaultRecoveryCaption: true, idVaultFileCaption: true,
	idAutoLockCaption: true, idSetTypeCaption: true, idSetSocketCaption: true, idSetVaultCaption: true,
	idSetKeysCaption: true, idSetDirsCaption: true,
}

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
	// Right to left: what is done to the agent, then to its vault.
	for _, id := range []uint16{idMainAgentToggle, idMainRestart, idMainLock} {
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

	// The page tabs, side by side, the same width.
	tabY := margin + s(lyTitleH+lyLabelH) + gap
	tw := max(nat(idMainTabKeys, 90), nat(idMainTabVault, 90), nat(idMainTabSettings, 90))
	out[idMainTabKeys] = box{margin, tabY, tw, btnH}
	out[idMainTabVault] = box{margin + tw, tabY, tw, btnH}
	out[idMainTabSettings] = box{margin + 2*tw, tabY, tw, btnH}
	out[idMainTabLine] = box{margin, tabY + btnH, right - margin, max(s(1), 1)}

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
	out[idMainSelftest] = box{margin, footY, nat(idMainSelftest, 110), btnH}
	// The version is the least of the footer: it shortens, then goes.
	afterSelftest := margin + out[idMainSelftest].w + s(lyMargin)
	vw := max(min(nat(idMainVersion, 80), right-afterSelftest), 0)
	out[idMainVersion] = box{right - vw, footY + (btnH-s(lyLabelH))/2, vw, s(lyLabelH)}
	out[idMainStatus] = box{margin, statusY, right - margin, s(lyLabelH)}

	// Body: the list, and beside it the details pane.
	bodyY := barY + btnH + gap
	bodyH := max(statusY-gap-bodyY, 0)
	inner := right - margin
	listW := splitWidth(inner, m.dpi)
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

	page := box{margin, barY, inner, max(statusY-gap-barY, 0)}
	vaultLayout(out, page, m)
	settingsLayout(out, page, m)
	return out
}

// splitWidth is the width of the left pane of a body inner wide: five
// twelfths, within limits, leaving the right pane its narrowest.
func splitWidth(inner, dpi int) int {
	s := func(v int) int { return scale(v, dpi) }
	w := min(max((inner-s(lyMargin))*5/12, s(lyListMinW)), s(lyListMaxW))
	if inner-w-s(lyMargin) < s(lyDetailMinW) {
		w = max(inner-s(lyMargin)-s(lyDetailMinW), s(lyListMinW))
	}
	return w
}

// cardKit places a card page's parts, down a pane from y.
type cardKit struct {
	out     map[uint16]box
	m       layoutMetrics
	x, w, y int
}

func (k *cardKit) s(v int) int { return scale(v, k.m.dpi) }

func (k *cardKit) nat(id uint16, fallback int) int {
	if w, ok := k.m.natural[id]; ok {
		return w
	}
	return k.s(fallback)
}

// header places a section's header, after a gap unless it is the first.
func (k *cardKit) header(id uint16, first bool) {
	if !first {
		k.y += k.s(lySectionGap)
	}
	k.out[id] = box{k.x, k.y, k.w, k.s(lyLabelH)}
	k.y += k.s(lyHeaderH)
}

// line places a label a line high across the pane.
func (k *cardKit) line(id uint16) {
	k.out[id] = box{k.x, k.y, k.w, k.s(lyLabelH)}
	k.y += k.s(lyLabelH)
}

// card places a title over a caption at the left and controls, each ctrlW
// wide, at the right, centred on the card.
func (k *cardKit) card(title, caption uint16, ctrlW int, controls ...uint16) {
	gap, h := k.s(lyGap), k.s(lyCardH)
	cx := k.x + k.w
	for i := len(controls) - 1; i >= 0; i-- {
		cx -= ctrlW
		k.out[controls[i]] = box{cx, k.y + (h-k.s(lyButtonH))/2, ctrlW, k.s(lyButtonH)}
		cx -= gap
	}
	tw := max(cx-k.x, 0)
	k.out[title] = box{k.x, k.y + (h-2*k.s(lyLabelH))/2, tw, k.s(lyLabelH)}
	k.out[caption] = box{k.x, k.y + h/2, tw, k.s(lyLabelH)}
	k.y += h
}

// field places a title over an edit box, with a button at its right, and a
// caption below, which says what is wrong with it when something is.
func (k *cardKit) field(title, edit, button, caption uint16, btnW int) {
	gap, btnH, fieldH := k.s(lyGap), k.s(lyButtonH), k.s(lyFieldH)
	k.line(title)
	k.y += k.s(4)
	k.out[button] = box{k.x + k.w - btnW, k.y, btnW, btnH}
	k.out[edit] = box{k.x, k.y + (btnH-fieldH)/2, max(k.w-btnW-gap, 0), fieldH}
	k.y += btnH + k.s(2)
	k.line(caption)
}

// widest is the widest of ids' natural widths, and at least a card button's.
func (k *cardKit) widest(ids ...uint16) int {
	w := k.s(lyCardBtnW)
	for _, id := range ids {
		w = max(w, k.nat(id, lyCardBtnW))
	}
	return w
}

// vaultLayout places the Vault page in body: on the left its state, with
// the primary button, and its facts; on the right its cards.
func vaultLayout(out map[uint16]box, body box, m layoutMetrics) {
	s := func(v int) int { return scale(v, m.dpi) }
	gap, btnH := s(lyGap), s(lyButtonH)
	lw := splitWidth(body.w, m.dpi)
	left := &cardKit{out: out, m: m, x: body.x, w: lw, y: body.y}

	pw := left.widest(idMainCreateVault)
	out[idMainCreateVault] = box{body.x + lw - pw, body.y, pw, btnH}
	out[idVaultTitle] = box{body.x, body.y, max(lw-pw-gap, 0), s(lyDetailH)}
	// The explanation starts below the button, and may wrap twice.
	out[idVaultSub] = box{body.x, body.y + btnH + s(4), lw, 3 * s(lyLabelH)}
	y := body.y + btnH + s(4) + 3*s(lyLabelH) + s(lyMargin)

	labelW := min(s(lyRowLabelW), lw/3)
	for i := range vaultRowCount {
		out[vaultRowLabel(i)] = box{body.x, y, labelW, s(lyLabelH)}
		out[vaultRowValue(i)] = box{body.x + labelW + gap, y, max(lw-labelW-gap, 0), s(lyLabelH)}
		if !m.hidden[vaultRowLabel(i)] {
			y += s(lyRowH)
		}
	}
	y += s(lyGap)
	x := body.x
	for _, id := range []uint16{idVaultOpenFolder, idVaultCopyPath} {
		w := left.nat(id, 110)
		out[id] = box{x, y, w, btnH}
		x += w + gap
	}

	rx := body.x + lw + s(lyMargin)
	k := &cardKit{out: out, m: m, x: rx, w: max(body.x+body.w-rx, 0), y: body.y}
	bw := k.widest(vaultCardButtons...)
	k.header(idVaultSecHeader, true)
	k.card(idVaultPasswdTitle, idVaultPasswdCaption, bw, idVaultPasswd)
	k.card(idVaultRecoveryTitle, idVaultRecoveryCaption, bw, idVaultRecovery)

	k.header(idAutoLockTitle, false)
	k.line(idAutoLockCaption)
	for _, id := range []uint16{idAutoLockSession, idAutoLockSleep} {
		out[id] = box{k.x, k.y, k.w, s(24)}
		k.y += s(24)
	}
	// A drop-down's height is its list's when open; the closed box is as
	// tall as the font makes it, about a field.
	cw := min(bw, k.w)
	out[idAutoLockIdle] = box{k.x + k.w - cw, k.y + s(2), cw, s(160)}
	out[idAutoLockIdleLabel] = box{k.x, k.y + s(2) + (s(lyFieldH)-s(lyLabelH))/2, max(k.w-cw-gap, 0), s(lyLabelH)}
	k.y += s(lyFieldH) + s(4)

	k.header(idVaultFileHeader, false)
	k.card(idVaultFileTitle, idVaultFileCaption, bw, idVaultBackup, idVaultImport)
}

// settingsLayout places the Settings page in body: the agent, the vault and
// the tray's own settings on the left; the lists of key files and search
// folders on the right, sharing its height, then Save.
func settingsLayout(out map[uint16]box, body box, m layoutMetrics) {
	s := func(v int) int { return scale(v, m.dpi) }
	gap, btnH := s(lyGap), s(lyButtonH)
	lw := (body.w - s(lyMargin)) / 2
	k := &cardKit{out: out, m: m, x: body.x, w: lw, y: body.y}
	bw := k.widest(idSetSocketDefault, idSetVaultBrowse)

	k.header(idSetAgentHeader, true)
	comboW := min(s(lyComboW+28), k.w)
	k.card(idSetTypeTitle, idSetTypeCaption, comboW, idSetType)
	// The drop-down: closed, a field high, centred; open, its list.
	t := out[idSetType]
	out[idSetType] = box{t.x, t.y + (t.h-s(lyFieldH))/2, t.w, s(160)}
	k.y += s(6)
	k.field(idSetSocketTitle, idSetSocket, idSetSocketDefault, idSetSocketCaption, bw)

	k.header(idSetVaultHeader, false)
	k.field(idSetVaultTitle, idSetVaultPath, idSetVaultBrowse, idSetVaultCaption, bw)

	k.header(idSetTrayHeader, false)
	out[idMainTheme] = box{k.x + k.w - comboW, k.y + s(3), comboW, s(160)}
	out[idMainThemeLabel] = box{k.x, k.y + s(3) + (s(lyFieldH)-s(lyLabelH))/2, max(k.w-comboW-gap, 0), s(lyLabelH)}
	k.y += btnH
	out[idMainStartup] = box{k.x, k.y, k.w, s(24)}

	// The right: two lists that share what height there is, and Save last.
	rx := body.x + lw + s(lyMargin)
	rw := max(body.x+body.w-rx, 0)
	bottom := body.y + body.h
	sw := k.nat(idSetSave, 90)
	out[idSetSave] = box{rx + rw - sw, bottom - btnH, sw, btnH}
	dw := k.nat(idSetDiscard, 120)
	out[idSetDiscard] = box{out[idSetSave].x - gap - dw, bottom - btnH, dw, btnH}
	each := s(lyHeaderH) + s(lyLabelH) + s(4) + gap + btnH
	listH := max((bottom-btnH-s(lyMargin)-body.y-s(lySectionGap)-2*each)/2, s(40))
	r := &cardKit{out: out, m: m, x: rx, w: rw, y: body.y}
	list := func(header, caption, lst, add, remove uint16, first bool) {
		r.header(header, first)
		r.line(caption)
		r.y += s(4)
		out[lst] = box{rx, r.y, rw, listH}
		r.y += listH + gap
		aw := r.nat(add, 110)
		out[add] = box{rx, r.y, aw, btnH}
		out[remove] = box{rx + aw + gap, r.y, r.nat(remove, 90), btnH}
		r.y += btnH
	}
	list(idSetKeysHeader, idSetKeysCaption, idSetKeysList, idSetKeysAdd, idSetKeysRemove, true)
	list(idSetDirsHeader, idSetDirsCaption, idSetDirsList, idSetDirsAdd, idSetDirsRemove, false)
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
