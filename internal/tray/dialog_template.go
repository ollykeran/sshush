package tray

import "unicode/utf16"

// The passphrase dialog and the vault window are in-memory DLGTEMPLATEs handed
// to Windows' dialog manager, which then supplies Tab, Enter and Esc handling
// and scales the layout with the font. Building the template is plain byte layout, so it
// lives here and is tested on every OS.

// Dialog item IDs. IDOK and IDCANCEL are the ones the dialog manager sends for
// Enter and Esc.
const (
	idOK      = 1
	idCancel  = 2
	idMessage = 100
	idEdit    = 101
	idError   = 102
)

// Window and dialog styles the template uses.
const (
	wsChild         = 0x40000000
	wsVisible       = 0x10000000
	wsPopup         = 0x80000000
	wsCaption       = 0x00C00000
	wsSysMenu       = 0x00080000
	wsTabStop       = 0x00010000
	wsExClientEdge  = 0x00000200
	dsSetFont       = 0x40
	dsModalFrame    = 0x80
	dsSetForeground = 0x200
	dsCenter        = 0x800
	esPassword      = 0x20
	esAutoHScroll   = 0x80
	bsDefPush       = 0x1
	ssNoPrefix      = 0x80
)

// Predefined window class atoms for dialog items.
const (
	classButton = 0x80
	classEdit   = 0x81
	classStatic = 0x82
)

// dlgItem is one control in a dialog template, in dialog units.
type dlgItem struct {
	style, exStyle uint32
	x, y, cx, cy   int16
	id             uint16
	class          uint16 // predefined class atom, used when className is empty
	className      string // registered window class, such as a common control's
	text           string
}

// passphraseTemplate lays out the unlock dialog: message, masked field, an
// error line (empty until a wrong passphrase), and OK / Cancel.
func passphraseTemplate(title, message, errText string) []uint16 {
	items := []dlgItem{
		{style: wsChild | wsVisible | ssNoPrefix, x: 10, y: 10, cx: 200, cy: 18, id: idMessage, class: classStatic, text: message},
		{style: wsChild | wsVisible | wsTabStop | esPassword | esAutoHScroll, exStyle: wsExClientEdge, x: 10, y: 32, cx: 200, cy: 14, id: idEdit, class: classEdit},
		{style: wsChild | wsVisible | ssNoPrefix, x: 10, y: 50, cx: 200, cy: 10, id: idError, class: classStatic, text: errText},
		{style: wsChild | wsVisible | wsTabStop | bsDefPush, x: 106, y: 66, cx: 50, cy: 14, id: idOK, class: classButton, text: "Unlock"},
		{style: wsChild | wsVisible | wsTabStop, x: 160, y: 66, cx: 50, cy: 14, id: idCancel, class: classButton, text: "Cancel"},
	}
	style := uint32(wsPopup | wsCaption | wsSysMenu | dsSetFont | dsModalFrame | dsSetForeground | dsCenter)
	return buildTemplate(style, 220, 88, title, "Segoe UI", 9, items)
}

// buildTemplate encodes a DLGTEMPLATE followed by its DLGITEMTEMPLATEs as
// WORDs. Every item starts on a DWORD boundary relative to the template, so
// the caller must place the result at a DWORD-aligned address.
func buildTemplate(style uint32, cx, cy int16, title, font string, pointSize uint16, items []dlgItem) []uint16 {
	var b []uint16
	dword := func(v uint32) { b = append(b, uint16(v), uint16(v>>16)) }
	str := func(s string) { b = append(append(b, utf16.Encode([]rune(s))...), 0) }
	align := func() {
		if len(b)%2 != 0 {
			b = append(b, 0)
		}
	}

	dword(style)
	dword(0) // extended style
	b = append(b, uint16(len(items)), 0, 0, uint16(cx), uint16(cy))
	b = append(b, 0, 0) // no menu, default dialog class
	str(title)
	b = append(b, pointSize)
	str(font)

	for _, it := range items {
		align()
		dword(it.style)
		dword(it.exStyle)
		b = append(b, uint16(it.x), uint16(it.y), uint16(it.cx), uint16(it.cy), it.id)
		if it.className != "" {
			str(it.className)
		} else {
			b = append(b, 0xFFFF, it.class)
		}
		str(it.text)
		b = append(b, 0) // no creation data
	}
	return b
}
