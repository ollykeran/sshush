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
	idExtra   = 103 // an input dialog's optional third button

	// The create key dialog's own items.
	idCreateTypeLabel    = 110
	idCreateType         = 111
	idCreateCommentLabel = 112
	idCreateComment      = 113
	idCreatePathLabel    = 114
	idCreatePath         = 115
	idCreateBrowse       = 116
	idCreateAdd          = 117
)

// Window and dialog styles the templates use.
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

// inputSpec describes a one-field input dialog: the passphrase prompt,
// renaming a key, and the like.
type inputSpec struct {
	title, message, errText string
	// initial is the field's text when the dialog opens, selected.
	initial string
	// okLabel names the default button; "OK" when empty.
	okLabel string
	// extraLabel, when set, adds a button at the left that ends the dialog
	// with idExtra, for another way to answer, such as a recovery phrase.
	extraLabel string
	// masked hides what is typed, for a passphrase.
	masked bool
}

// inputTemplate lays out an input dialog: message, field, an error line
// (empty until something is wrong), and the buttons.
func inputTemplate(spec inputSpec) []uint16 {
	ok := spec.okLabel
	if ok == "" {
		ok = "OK"
	}
	edit := uint32(wsChild | wsVisible | wsTabStop | esAutoHScroll)
	if spec.masked {
		edit |= esPassword
	}
	items := []dlgItem{
		{style: wsChild | wsVisible | ssNoPrefix, x: 10, y: 10, cx: 220, cy: 26, id: idMessage, class: classStatic, text: spec.message},
		{style: edit, exStyle: wsExClientEdge, x: 10, y: 40, cx: 220, cy: 14, id: idEdit, class: classEdit},
		{style: wsChild | wsVisible | ssNoPrefix, x: 10, y: 58, cx: 220, cy: 10, id: idError, class: classStatic, text: spec.errText},
		{style: wsChild | wsVisible | wsTabStop | bsDefPush, x: 126, y: 74, cx: 50, cy: 14, id: idOK, class: classButton, text: ok},
		{style: wsChild | wsVisible | wsTabStop, x: 180, y: 74, cx: 50, cy: 14, id: idCancel, class: classButton, text: "Cancel"},
	}
	if spec.extraLabel != "" {
		items = append(items, dlgItem{style: wsChild | wsVisible | wsTabStop, x: 10, y: 74, cx: 90, cy: 14, id: idExtra, class: classButton, text: spec.extraLabel})
	}
	style := uint32(wsPopup | wsCaption | wsSysMenu | dsSetFont | dsModalFrame | dsSetForeground | dsCenter)
	return buildTemplate(style, 240, 96, spec.title, "Segoe UI", 9, items)
}

// passphraseTemplate lays out the unlock dialog.
func passphraseTemplate(title, message, errText string) []uint16 {
	return inputTemplate(inputSpec{title: title, message: message, errText: errText, okLabel: "Unlock", masked: true})
}

// phraseTemplate lays out the dialog that shows a new vault's recovery
// phrase once: what it is for, the words, selectable, and a way to copy them
// that keeps them out of clipboard history.
func phraseTemplate(message string) []uint16 {
	items := []dlgItem{
		{style: wsChild | wsVisible | ssNoPrefix, x: 10, y: 10, cx: 260, cy: 44, id: idMessage, class: classStatic, text: message},
		{style: wsChild | wsVisible | wsTabStop | esReadOnly | esMultiline, exStyle: wsExClientEdge, x: 10, y: 58, cx: 260, cy: 50, id: idEdit, class: classEdit},
		{style: wsChild | wsVisible | wsTabStop, x: 10, y: 116, cx: 60, cy: 14, id: idExtra, class: classButton, text: "&Copy"},
		{style: wsChild | wsVisible | wsTabStop | bsDefPush, x: 180, y: 116, cx: 90, cy: 14, id: idOK, class: classButton, text: "I have saved it"},
	}
	style := uint32(wsPopup | wsCaption | dsSetFont | dsModalFrame | dsSetForeground | dsCenter)
	return buildTemplate(style, 280, 138, "Your recovery phrase", "Segoe UI", 9, items)
}

// createKeyTemplate lays out the create key dialog: the kind of key, its
// name, where to save it, whether to add it now, and an error line. addLabel
// says where it would be added.
func createKeyTemplate(addLabel string) []uint16 {
	const stop = wsChild | wsVisible | wsTabStop
	label := func(id uint16, y int16, text string) dlgItem {
		return dlgItem{style: wsChild | wsVisible, x: 10, y: y, cx: 64, cy: 10, id: id, class: classStatic, text: text}
	}
	items := []dlgItem{
		label(idCreateTypeLabel, 12, "&Kind of key:"),
		{style: stop | wsVScroll | cbsDropDownList, x: 78, y: 10, cx: 182, cy: 80, id: idCreateType, class: classComboBox},
		label(idCreateCommentLabel, 32, "&Name:"),
		{style: stop | esAutoHScroll, exStyle: wsExClientEdge, x: 78, y: 30, cx: 182, cy: 14, id: idCreateComment, class: classEdit},
		label(idCreatePathLabel, 52, "&Save as:"),
		{style: stop | esAutoHScroll, exStyle: wsExClientEdge, x: 78, y: 50, cx: 128, cy: 14, id: idCreatePath, class: classEdit},
		{style: stop, x: 210, y: 50, cx: 50, cy: 14, id: idCreateBrowse, class: classButton, text: "&Browse…"},
		{style: stop | bsAutoCheckbox, x: 78, y: 70, cx: 182, cy: 12, id: idCreateAdd, class: classButton, text: addLabel},
		{style: wsChild | wsVisible | ssNoPrefix, x: 10, y: 88, cx: 250, cy: 10, id: idError, class: classStatic},
		{style: stop | bsDefPush, x: 156, y: 104, cx: 50, cy: 14, id: idOK, class: classButton, text: "Create"},
		{style: stop, x: 210, y: 104, cx: 50, cy: 14, id: idCancel, class: classButton, text: "Cancel"},
	}
	style := uint32(wsPopup | wsCaption | wsSysMenu | dsSetFont | dsModalFrame | dsSetForeground | dsCenter)
	return buildTemplate(style, 270, 126, "Create key", "Segoe UI", 9, items)
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
