package tray

import (
	"testing"
	"unicode/utf16"
)

// readTemplate walks a template the way the dialog manager does and returns
// each item's offset (in WORDs), ID, class and text. An item whose class is a
// name rather than an atom is reported as class 0, with the name in names.
func readTemplate(t *testing.T, b []uint16) (title string, offsets []int, ids []uint16, classes []uint16, texts []string) {
	title, offsets, ids, classes, texts, _ = readTemplateNames(t, b)
	return title, offsets, ids, classes, texts
}

func readTemplateNames(t *testing.T, b []uint16) (title string, offsets []int, ids []uint16, classes []uint16, texts, names []string) {
	t.Helper()
	pos := 0
	str := func() string {
		start := pos
		for b[pos] != 0 {
			pos++
		}
		pos++
		return string(utf16.Decode(b[start : pos-1]))
	}
	style := uint32(b[0]) | uint32(b[1])<<16
	count := int(b[4])
	pos = 2 + 2 + 5 // style, exStyle, cdit x y cx cy
	if b[pos] != 0 || b[pos+1] != 0 {
		t.Fatalf("menu and class: got %d %d, want 0 0", b[pos], b[pos+1])
	}
	pos += 2
	title = str()
	if style&dsSetFont != 0 {
		pos++ // point size
		str()
	}
	for range count {
		if pos%2 != 0 {
			pos++
		}
		offsets = append(offsets, pos)
		pos += 2 + 2 + 4 // style, exStyle, x y cx cy
		ids = append(ids, b[pos])
		pos++
		if b[pos] == 0xFFFF {
			classes = append(classes, b[pos+1])
			names = append(names, "")
			pos += 2
		} else {
			classes = append(classes, 0)
			names = append(names, str())
		}
		texts = append(texts, str())
		if b[pos] != 0 {
			t.Fatalf("item %d: unexpected creation data", len(ids)-1)
		}
		pos++
	}
	if pos != len(b) {
		t.Fatalf("template has %d trailing words", len(b)-pos)
	}
	return title, offsets, ids, classes, texts, names
}

func TestPassphraseTemplateLayout(t *testing.T) {
	b := passphraseTemplate("sshush", "Enter the passphrase.", "Wrong passphrase.")
	title, offsets, ids, classes, texts := readTemplate(t, b)
	if title != "sshush" {
		t.Errorf("title: got %q", title)
	}
	for i, off := range offsets {
		if off%2 != 0 {
			t.Errorf("item %d starts at word %d, not on a DWORD boundary", i, off)
		}
	}
	wantIDs := []uint16{idMessage, idEdit, idError, idOK, idCancel}
	wantClasses := []uint16{classStatic, classEdit, classStatic, classButton, classButton}
	wantTexts := []string{"Enter the passphrase.", "", "Wrong passphrase.", "Unlock", "Cancel"}
	if len(ids) != len(wantIDs) {
		t.Fatalf("items: got %d, want %d", len(ids), len(wantIDs))
	}
	for i := range wantIDs {
		if ids[i] != wantIDs[i] || classes[i] != wantClasses[i] || texts[i] != wantTexts[i] {
			t.Errorf("item %d: got id %d class %#x text %q, want id %d class %#x text %q",
				i, ids[i], classes[i], texts[i], wantIDs[i], wantClasses[i], wantTexts[i])
		}
	}
}

func TestPassphraseTemplateEditIsMasked(t *testing.T) {
	b := passphraseTemplate("sshush", "m", "")
	_, offsets, ids, _, _ := readTemplate(t, b)
	for i, id := range ids {
		if id != idEdit {
			continue
		}
		style := uint32(b[offsets[i]]) | uint32(b[offsets[i]+1])<<16
		if style&esPassword == 0 {
			t.Error("passphrase field is not ES_PASSWORD")
		}
		return
	}
	t.Fatal("no edit control in template")
}

func TestTemplateItemByClassName(t *testing.T) {
	b := buildTemplate(wsPopup|dsSetFont, 100, 100, "t", "Segoe UI", 9, []dlgItem{
		{id: 1, className: classListView},
		{id: 2, class: classButton, text: "OK"},
	})
	_, offsets, ids, classes, texts, names := readTemplateNames(t, b)
	if len(ids) != 2 || names[0] != classListView || classes[0] != 0 || texts[0] != "" {
		t.Fatalf("named class item: ids %v classes %v names %q texts %q", ids, classes, names, texts)
	}
	if classes[1] != classButton || names[1] != "" || texts[1] != "OK" {
		t.Errorf("atom item after a named one: class %#x name %q text %q", classes[1], names[1], texts[1])
	}
	for i, off := range offsets {
		if off%2 != 0 {
			t.Errorf("item %d starts at word %d, not on a DWORD boundary", i, off)
		}
	}
}
