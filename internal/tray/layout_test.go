package tray

import "testing"

func overlaps(a, b box) bool {
	return a.x < b.x+b.w && b.x < a.x+a.w && a.y < b.y+b.h && b.y < a.y+a.h
}

func TestMainLayoutFitsAtEverySize(t *testing.T) {
	for _, dpi := range []int{96, 144, 192} {
		m := layoutMetrics{dpi: dpi, pubLines: 3}
		mw, mh := mainMinSize(m)
		for _, size := range [][2]int{{mw, mh}, {mw + 300, mh + 200}, {3 * mw, 2 * mh}} {
			boxes := mainLayout(size[0], size[1], m)
			for id, b := range boxes {
				h := b.h
				if isDropDown(id) {
					h = scale(lyFieldH, dpi) // the rest is its drop-down
				}
				if id == idMainVersion && b.w == 0 {
					continue // it gives way when there is no room
				}
				if b.x < 0 || b.y < 0 || b.w <= 0 || h <= 0 || b.x+b.w > size[0] || b.y+h > size[1] {
					t.Errorf("dpi %d, %dx%d: control %d at %+v falls outside the window", dpi, size[0], size[1], id, b)
				}
			}
			// The details pane sits beside the list, its content above the status line.
			list, status := boxes[idMainList], boxes[idMainStatus]
			for _, id := range append([]uint16{idMainDetailTitle, idMainFp, idMainPub, idMainCopyPub, idMainRemove}, detailButtons...) {
				b := boxes[id]
				if overlaps(b, list) {
					t.Errorf("dpi %d, %dx%d: control %d overlaps the list", dpi, size[0], size[1], id)
				}
				if b.y+b.h > status.y {
					t.Errorf("dpi %d, %dx%d: control %d runs into the status line", dpi, size[0], size[1], id)
				}
			}
			// No two buttons overlap.
			var buttons []uint16
			for id := range buttonGlyphs {
				buttons = append(buttons, id)
			}
			buttons = append(buttons, idMainLock, idMainAgentToggle, idMainLoad, idMainTabKeys, idMainTabVault, idMainTabSettings,
				idSetSocketDefault, idSetVaultBrowse, idSetKeysAdd, idSetKeysRemove, idSetDirsAdd, idSetDirsRemove, idSetSave, idSetDiscard)
			for i, a := range buttons {
				for _, b := range buttons[i+1:] {
					// The pages share the body; only one is shown.
					if pageOfControl(a) != pageOfControl(b) {
						continue
					}
					if overlaps(boxes[a], boxes[b]) {
						t.Errorf("dpi %d, %dx%d: buttons %d and %d overlap", dpi, size[0], size[1], a, b)
					}
				}
			}
		}
	}
}

func TestMainLayoutGivesSpaceToTheList(t *testing.T) {
	m := layoutMetrics{dpi: 96}
	small := mainLayout(900, 600, m)
	big := mainLayout(1400, 900, m)
	if big[idMainList].h <= small[idMainList].h {
		t.Error("a taller window does not make the list taller")
	}
	if big[idMainPub].w <= small[idMainPub].w {
		t.Error("a wider window does not widen the details")
	}
	if big[idMainList].w > scale(lyListMaxW, 96) {
		t.Errorf("the list grew to %d, past its widest", big[idMainList].w)
	}
}

func TestMainLayoutHiddenButtonsGiveUpTheirPlace(t *testing.T) {
	m := layoutMetrics{dpi: 96, hidden: map[uint16]bool{idMainAdd: true}}
	boxes := mainLayout(1000, 700, m)
	if boxes[idMainCreateKey].x != scale(lyMargin, 96) {
		t.Errorf("create key at x %d, want it where Add was", boxes[idMainCreateKey].x)
	}
	right := 1000 - scale(lyMargin, 96)
	if b := boxes[idMainAgentToggle]; b.x+b.w != right {
		t.Errorf("start/stop ends at %d, want the margin at %d", b.x+b.w, right)
	}
}

func TestMainLayoutWrapsNarrowDetails(t *testing.T) {
	wide := map[uint16]int{}
	for _, id := range append(detailButtons, idMainRemove) {
		wide[id] = 160
	}
	m := layoutMetrics{dpi: 96, natural: wide}
	mw, mh := mainMinSize(m)
	boxes := mainLayout(mw, mh, m)
	first, last := boxes[detailButtons[0]], boxes[idMainRemove]
	if last.y <= first.y {
		t.Error("buttons too wide for one row did not wrap")
	}
	dx := boxes[idMainDetailTitle].x
	for _, id := range append(detailButtons, idMainRemove) {
		if b := boxes[id]; b.x < dx || b.x+b.w > dx+boxes[idMainDetailTitle].w {
			t.Errorf("button %d at %+v leaves the details pane", id, b)
		}
	}
}

func TestPublicKeyFieldGrowsWithTheKey(t *testing.T) {
	short := mainLayout(1000, 700, layoutMetrics{dpi: 96, pubLines: 1})
	long := mainLayout(1000, 700, layoutMetrics{dpi: 96, pubLines: 5})
	huge := mainLayout(1000, 700, layoutMetrics{dpi: 96, pubLines: 50})
	if long[idMainPub].h <= short[idMainPub].h {
		t.Error("a key that wraps to more lines does not get a taller field")
	}
	if huge[idMainPub].h != long[idMainPub].h+scale(lyLineH, 96) {
		t.Errorf("field for a huge key is %d tall; it should stop at %d lines and scroll", huge[idMainPub].h, lyPubMaxLine)
	}
}

// pageOfControl is the page a control is on: 0 for Keys and what every page
// shares, 1 for Vault, 2 for Settings.
func pageOfControl(id uint16) int {
	switch {
	case vaultPage[id]:
		return 1
	case settingsPage[id]:
		return 2
	}
	return 0
}

func isDropDown(id uint16) bool {
	return id == idMainTheme || id == idAutoLockIdle || id == idSetType
}

func TestFooterGivesWayToSelftest(t *testing.T) {
	m := layoutMetrics{dpi: 96, natural: map[uint16]int{idMainVersion: 900}}
	mw, mh := mainMinSize(m)
	boxes := mainLayout(mw, mh, m)
	if a, b := boxes[idMainSelftest], boxes[idMainVersion]; b.w > 0 && overlaps(a, b) {
		t.Errorf("the version %+v runs into selftest %+v", b, a)
	}
}

// TestCardPagesFitAtEverySize checks the Vault and Settings pages: inside the
// window, between the tabs and the status line, and nothing on a page over
// anything else on it.
func TestCardPagesFitAtEverySize(t *testing.T) {
	for _, page := range []map[uint16]bool{vaultPage, settingsPage} {
		for _, dpi := range []int{96, 144, 192} {
			m := layoutMetrics{dpi: dpi}
			mw, mh := mainMinSize(m)
			for _, size := range [][2]int{{mw, mh}, {mw + 300, mh + 200}, {3 * mw, 2 * mh}} {
				boxes := mainLayout(size[0], size[1], m)
				status, tabs := boxes[idMainStatus], boxes[idMainTabVault]
				var ids []uint16
				for id := range page {
					ids = append(ids, id)
				}
				closed := func(id uint16) box {
					b := boxes[id]
					if isDropDown(id) {
						b.h = scale(lyFieldH, dpi)
					}
					return b
				}
				for i, a := range ids {
					b := closed(a)
					if b.x < 0 || b.w <= 0 || b.x+b.w > size[0] {
						t.Errorf("dpi %d, %dx%d: control %d at %+v falls outside the window", dpi, size[0], size[1], a, b)
					}
					if b.y < tabs.y+tabs.h || b.y+b.h > status.y {
						t.Errorf("dpi %d, %dx%d: control %d at %+v is not between the tabs and the status line", dpi, size[0], size[1], a, b)
					}
					for _, c := range ids[i+1:] {
						if overlaps(b, closed(c)) {
							t.Errorf("dpi %d, %dx%d: controls %d and %d overlap", dpi, size[0], size[1], a, c)
						}
					}
				}
			}
		}
	}
}

func TestVaultCardButtonsLineUp(t *testing.T) {
	for _, width := range []int{900, 1400} {
		boxes := mainLayout(width, 700, layoutMetrics{dpi: 96, natural: map[uint16]int{idVaultPasswd: 90, idVaultMode: 150}})
		right, w := boxes[idVaultPasswd].x+boxes[idVaultPasswd].w, boxes[idVaultPasswd].w
		for _, id := range []uint16{idVaultRecovery, idVaultImport, idVaultMode} {
			if b := boxes[id]; b.x+b.w != right || b.w != w {
				t.Errorf("width %d: card button %d at %+v; want %d wide, ending at %d", width, id, b, w, right)
			}
		}
		if w < 150 {
			t.Errorf("card buttons are %d wide, narrower than the widest's 150", w)
		}
		if boxes[idVaultBackup].w != w {
			t.Error("the backup button is not as wide as the rest")
		}
	}
}

func TestVaultLayoutClosesUpHiddenRows(t *testing.T) {
	all := mainLayout(1000, 700, layoutMetrics{dpi: 96})
	hidden := map[uint16]bool{}
	for i := 3; i < vaultRowCount; i++ {
		hidden[vaultRowLabel(i)], hidden[vaultRowValue(i)] = true, true
	}
	few := mainLayout(1000, 700, layoutMetrics{dpi: 96, hidden: hidden})
	if few[idVaultOpenFolder].y >= all[idVaultOpenFolder].y {
		t.Errorf("hiding rows does not move the file's buttons up: %d, then %d", all[idVaultOpenFolder].y, few[idVaultOpenFolder].y)
	}
	if few[idVaultOpenFolder].y <= few[vaultRowValue(2)].y {
		t.Error("the file's buttons are not below the last row shown")
	}
	if few[idMainLock] != all[idMainLock] || few[idVaultPasswd] != all[idVaultPasswd] {
		t.Error("the primary button or the cards moved with the facts")
	}
}

func TestVaultPrimaryButtonInTheCorner(t *testing.T) {
	boxes := mainLayout(1000, 700, layoutMetrics{dpi: 96})
	p, title := boxes[idMainLock], boxes[idVaultTitle]
	if p.y != title.y || p.x <= title.x+title.w-1 {
		t.Errorf("primary %+v is not at the right of the title %+v", p, title)
	}
	if p.x+p.w > boxes[idVaultSecHeader].x {
		t.Error("the primary button runs into the cards")
	}
}

func TestSettingsListsShareTheHeight(t *testing.T) {
	small := mainLayout(900, 640, layoutMetrics{dpi: 96})
	big := mainLayout(900, 900, layoutMetrics{dpi: 96})
	if big[idSetKeysList].h <= small[idSetKeysList].h {
		t.Error("a taller window does not make the lists taller")
	}
	if big[idSetKeysList].h != big[idSetDirsList].h {
		t.Error("the two lists are not the same height")
	}
	if s := big[idSetSave]; s.y+s.h < big[idSetDirsRemove].y+big[idSetDirsRemove].h {
		t.Error("Save is not below the lists")
	}
}
