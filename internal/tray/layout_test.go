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
				if id == idMainTheme {
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
			buttons = append(buttons, idMainLock, idMainAgentToggle, idMainLoad, idMainTabKeys, idMainTabVault)
			for i, a := range buttons {
				for _, b := range buttons[i+1:] {
					// The two pages share the body; only one is shown.
					if vaultPage[a] != vaultPage[b] {
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
	m := layoutMetrics{dpi: 96, hidden: map[uint16]bool{idMainAdd: true, idMainLock: true}}
	boxes := mainLayout(1000, 700, m)
	if boxes[idMainCreateVault].x != scale(lyMargin, 96) {
		t.Errorf("create vault at x %d, want it where Add was", boxes[idMainCreateVault].x)
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

func TestFooterControlsDoNotOverlap(t *testing.T) {
	// A long development version must give way to the settings.
	m := layoutMetrics{dpi: 96, natural: map[uint16]int{idMainVersion: 400, idMainStartup: 140}}
	mw, mh := mainMinSize(m)
	for _, w := range []int{mw, mw + 200, mw + 800} {
		boxes := mainLayout(w, mh, m)
		footer := []uint16{idMainSelftest, idMainThemeLabel, idMainTheme, idMainStartup, idMainVersion}
		for i, a := range footer {
			for _, b := range footer[i+1:] {
				ba, bb := boxes[a], boxes[b]
				ba.h, bb.h = 1, 1 // the drop-down's height is its open list
				if ba.w > 0 && bb.w > 0 && overlaps(ba, bb) {
					t.Errorf("width %d: footer controls %d and %d overlap", w, a, b)
				}
			}
		}
	}
}

func TestVaultLayoutFitsAtEverySize(t *testing.T) {
	for _, dpi := range []int{96, 144, 192} {
		m := layoutMetrics{dpi: dpi}
		mw, mh := mainMinSize(m)
		for _, size := range [][2]int{{mw, mh}, {mw + 300, mh + 200}} {
			boxes := mainLayout(size[0], size[1], m)
			status, tabs := boxes[idMainStatus], boxes[idMainTabVault]
			var ids []uint16
			for id := range vaultPage {
				ids = append(ids, id)
			}
			for i, a := range ids {
				b := boxes[a]
				if b.x < 0 || b.w <= 0 || b.x+b.w > size[0] {
					t.Errorf("dpi %d, %dx%d: vault control %d at %+v falls outside the window", dpi, size[0], size[1], a, b)
				}
				if b.y < tabs.y+tabs.h || b.y+b.h > status.y {
					t.Errorf("dpi %d, %dx%d: vault control %d at %+v is not between the tabs and the status line", dpi, size[0], size[1], a, b)
				}
				for _, c := range ids[i+1:] {
					if overlaps(b, boxes[c]) {
						t.Errorf("dpi %d, %dx%d: vault controls %d and %d overlap", dpi, size[0], size[1], a, c)
					}
				}
			}
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
	if few[idMainLock].y >= all[idMainLock].y {
		t.Errorf("hiding rows does not move the buttons up: %d, then %d", all[idMainLock].y, few[idMainLock].y)
	}
	if few[idMainLock].y <= few[vaultRowValue(2)].y {
		t.Error("the buttons are not below the last row shown")
	}
}

func TestVaultLayoutCreateAndLockShareAPlace(t *testing.T) {
	m := layoutMetrics{dpi: 96, hidden: map[uint16]bool{idMainLock: true}}
	create := mainLayout(1000, 700, m)[idMainCreateVault]
	m.hidden = map[uint16]bool{idMainCreateVault: true}
	lock := mainLayout(1000, 700, m)[idMainLock]
	if create.x != lock.x || create.y != lock.y {
		t.Errorf("create vault at %+v, lock at %+v: want the same place", create, lock)
	}
}
