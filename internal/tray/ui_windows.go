package tray

import (
	"strings"
	"sync"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Drawing resources for the main window: icons drawn from Windows' icon font
// for the buttons, the tray's state icon at window size, and the header's
// larger font. Each is made for the window's DPI, and for the theme where
// its colour depends on it; uiCache owns them and frees them together.
var (
	procGetDpiForWindow      = user32.NewProc("GetDpiForWindow")
	procCreateIconIndirect   = user32.NewProc("CreateIconIndirect")
	procCreateIconFromResEx  = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon          = user32.NewProc("DestroyIcon")
	procDrawTextW            = user32.NewProc("DrawTextW")
	procSetWindowLongPtrW    = user32.NewProc("SetWindowLongPtrW")
	procCreateCompatibleDC   = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC             = gdi32.NewProc("DeleteDC")
	procCreateDIBSection     = gdi32.NewProc("CreateDIBSection")
	procCreateBitmap         = gdi32.NewProc("CreateBitmap")
	procCreateFontW          = gdi32.NewProc("CreateFontW")
	procSelectObject         = gdi32.NewProc("SelectObject")
	procDeleteObject         = gdi32.NewProc("DeleteObject")
	procGdiFlush             = gdi32.NewProc("GdiFlush")
	procGetTextFaceW         = gdi32.NewProc("GetTextFaceW")
	procGetTextExtentPoint   = gdi32.NewProc("GetTextExtentPoint32W")
	procGetDC                = user32.NewProc("GetDC")
	procReleaseDC            = user32.NewProc("ReleaseDC")
	procGetWindowRect        = user32.NewProc("GetWindowRect")
	procImageListCreate      = comctl32.NewProc("ImageList_Create")
	procImageListReplaceIcon = comctl32.NewProc("ImageList_ReplaceIcon")
	procImageListDestroy     = comctl32.NewProc("ImageList_Destroy")
	iconFaceOnce             sync.Once
	iconFace                 string
	iconFaceCandidates       = []string{"Segoe Fluent Icons", "Segoe MDL2 Assets"}
	monoFaceCandidates       = []string{"Cascadia Mono", "Consolas"}
	monoFaceOnce             sync.Once
	monoFace                 string
	glyphPixels, statePixels = 16, 32 // at 96 DPI
	headerPoints             = 15
	titlePoints              = 12
)

const (
	wmSetFont         = 0x0030
	bcmSetImageList   = 0x1602
	ilcColor32        = 0x0020
	buttonImageLeft   = 0
	glyphGap          = 4  // between a button's icon and its text, at 96 DPI
	buttonPad         = 14 // either side of a button's content, at 96 DPI
	buttonMinWidth    = 76
	wmGetFont         = 0x0031
	stmSetIcon        = 0x0170
	dwlpMsgResult     = 0
	fwNormal          = 400
	fwSemibold        = 600
	defaultCharset    = 1
	antialiasQuality  = 4 // greyscale, so a glyph's coverage is its alpha
	cleartypeQuality  = 5
	dtCenter          = 0x01
	dtVCenter         = 0x04
	dtSingleLine      = 0x20
	dtNoPrefix        = 0x800
	iconResourceVer   = 0x00030000
	nmCustomDraw      = 0xFFFFFFF4 // NM_FIRST - 12
	cddsPrePaint      = 0x00000001
	cddsItemPrePaint  = 0x00010001
	cdrfDoDefault     = 0x00
	cdrfNewFont       = 0x02
	cdrfNotifyItemDrw = 0x20
)

// nmLVCustomDraw is NMLVCUSTOMDRAW up to the colours, all this reads or sets.
type nmLVCustomDraw struct {
	hdr       nmHdr
	drawStage uint32
	hdc       uintptr
	_         [4]int32 // rc
	itemSpec  uintptr
	_         uint32  // uItemState
	_         uintptr // lItemParam
	clrText   colorRef
	_         colorRef // clrTextBk
}

type bitmapInfoHeader struct {
	size                     uint32
	width, height            int32
	planes, bitCount         uint16
	compression, sizeImage   uint32
	xPelsPerMeter, yPelsPerM int32
	clrUsed, clrImportant    uint32
}

type iconInfo struct {
	fIcon              int32
	xHotspot, yHotspot uint32
	hbmMask, hbmColor  uintptr
}

// setMsgResult is how a dialog procedure answers a WM_NOTIFY that wants a value.
func setMsgResult(hwnd, v uintptr) uintptr {
	procSetWindowLongPtrW.Call(hwnd, dwlpMsgResult, v)
	return 1
}

func dpiOf(hwnd uintptr) int {
	if procGetDpiForWindow.Find() == nil {
		if d, _, _ := procGetDpiForWindow.Call(hwnd); d != 0 {
			return int(d)
		}
	}
	return 96
}

// glyphKey names one drawn button icon.
type glyphKey struct {
	glyph rune
	color colorRef
}

// uiCache holds one window's drawing resources. It belongs to the window's
// thread.
type uiCache struct {
	dpi    int
	glyphs map[glyphKey]uintptr
	states map[Icon]uintptr
	header uintptr
	title  uintptr
	mono   uintptr
}

func newUICache(hwnd uintptr) *uiCache {
	return &uiCache{dpi: dpiOf(hwnd), glyphs: map[glyphKey]uintptr{}, states: map[Icon]uintptr{}}
}

// buttonImageList is BUTTON_IMAGELIST.
type buttonImageList struct {
	himl   uintptr
	margin [4]int32 // left, top, right, bottom
	align  uint32
}

// glyphImages is glyph drawn in color as a one-image list for a button, or 0
// when this Windows has no icon font. A button's image list, unlike a plain
// icon, puts a margin between the icon and the text.
func (u *uiCache) glyphImages(glyph rune, color colorRef) uintptr {
	k := glyphKey{glyph, color}
	if h, ok := u.glyphs[k]; ok {
		return h
	}
	px := scale(glyphPixels, u.dpi)
	var list uintptr
	if icon := renderGlyph(glyph, px, color); icon != 0 {
		list, _, _ = procImageListCreate.Call(uintptr(px), uintptr(px), ilcColor32, 1, 0)
		if list != 0 {
			procImageListReplaceIcon.Call(list, ^uintptr(0), icon) // -1 appends; the list copies the icon
		}
		procDestroyIcon.Call(icon)
	}
	u.glyphs[k] = list
	return list
}

// setButtonGlyph shows the image list on the button, left of its text.
func (u *uiCache) setButtonGlyph(button, list uintptr) {
	// The left margin matches the padding fitRows gives the button, less the
	// button's own border, so icon and label sit together as one group.
	left := int32(scale(buttonPad-4, u.dpi))
	bil := buttonImageList{himl: list, margin: [4]int32{left, 0, int32(scale(glyphGap, u.dpi)), 0}, align: buttonImageLeft}
	procSendMessageW.Call(button, bcmSetImageList, 0, uintptr(unsafe.Pointer(&bil)))
}

// stateIcon is the tray's icon for i at the header's size.
func (u *uiCache) stateIcon(i Icon) uintptr {
	if h, ok := u.states[i]; ok {
		return h
	}
	px := scale(statePixels, u.dpi)
	var h uintptr
	if img, _, err := icoImage(iconFor(i), px); err == nil {
		h, _, _ = procCreateIconFromResEx.Call(uintptr(unsafe.Pointer(&img[0])), uintptr(len(img)), 1, iconResourceVer, uintptr(px), uintptr(px), 0)
	}
	u.states[i] = h
	return h
}

// headerFont is the header's larger, semibold font.
func (u *uiCache) headerFont() uintptr {
	if u.header == 0 {
		u.header = createFont("Segoe UI", -scale(headerPoints*96/72, u.dpi), fwSemibold, cleartypeQuality)
	}
	return u.header
}

// titleFont is the details pane's semibold heading font.
func (u *uiCache) titleFont() uintptr {
	if u.title == 0 {
		u.title = createFont("Segoe UI", -scale(titlePoints*96/72, u.dpi), fwSemibold, cleartypeQuality)
	}
	return u.title
}

// monoFont is the fixed-width font key material is shown in, so a
// fingerprint or public key reads character by character.
func (u *uiCache) monoFont() uintptr {
	if u.mono == 0 {
		u.mono = createFont(findMonoFace(), -scale(9*96/72+1, u.dpi), fwNormal, cleartypeQuality)
	}
	return u.mono
}

// takeGlyphs hands over the button images drawn so far, to be freed with
// destroyImageLists once the buttons show new ones: the theme decides their
// colours.
func (u *uiCache) takeGlyphs() map[glyphKey]uintptr {
	old := u.glyphs
	u.glyphs = map[glyphKey]uintptr{}
	return old
}

func destroyImageLists(lists map[glyphKey]uintptr) {
	for _, h := range lists {
		if h != 0 {
			procImageListDestroy.Call(h)
		}
	}
}

func destroyIcons(icons map[Icon]uintptr) {
	for _, h := range icons {
		if h != 0 {
			procDestroyIcon.Call(h)
		}
	}
}

// release frees everything. The controls using them must be gone.
func (u *uiCache) release() {
	destroyImageLists(u.takeGlyphs())
	destroyIcons(u.states)
	clear(u.states)
	for _, f := range []*uintptr{&u.header, &u.title, &u.mono} {
		if *f != 0 {
			procDeleteObject.Call(*f)
			*f = 0
		}
	}
}

func createFont(face string, height, weight, quality int) uintptr {
	name, _ := windows.UTF16PtrFromString(face)
	f, _, _ := procCreateFontW.Call(uintptr(int32(height)), 0, 0, 0, uintptr(weight), 0, 0, 0,
		defaultCharset, 0, 0, uintptr(quality), 0, uintptr(unsafe.Pointer(name)))
	return f
}

// findIconFace returns the first icon font Windows has, or "".
func findIconFace() string {
	iconFaceOnce.Do(func() { iconFace = firstInstalledFace(iconFaceCandidates) })
	return iconFace
}

// findMonoFace returns the first fixed-width font Windows has; Consolas has
// shipped with every version sshush runs on.
func findMonoFace() string {
	monoFaceOnce.Do(func() {
		if monoFace = firstInstalledFace(monoFaceCandidates); monoFace == "" {
			monoFace = "Consolas"
		}
	})
	return monoFace
}

// firstInstalledFace returns the first of faces that is installed, or "". A
// font that is not installed is silently substituted, so each candidate is
// asked for and the face Windows actually chose compared with it.
func firstInstalledFace(faces []string) string {
	dc, _, _ := procCreateCompatibleDC.Call(0)
	defer procDeleteDC.Call(dc)
	for _, face := range faces {
		f := createFont(face, -16, fwNormal, antialiasQuality)
		old, _, _ := procSelectObject.Call(dc, f)
		var buf [64]uint16
		n, _, _ := procGetTextFaceW.Call(dc, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
		procSelectObject.Call(dc, old)
		procDeleteObject.Call(f)
		if n > 0 && windows.UTF16ToString(buf[:]) == face {
			return face
		}
	}
	return ""
}

// renderGlyph draws glyph from the icon font, px square, as an icon in color
// with antialiased edges. GDI draws text without alpha, so it is drawn white
// on black in greyscale and that coverage becomes the alpha of a solid colour.
func renderGlyph(glyph rune, px int, color colorRef) uintptr {
	face := findIconFace()
	if face == "" {
		return 0
	}
	dc, _, _ := procCreateCompatibleDC.Call(0)
	defer procDeleteDC.Call(dc)
	bmi := bitmapInfoHeader{width: int32(px), height: -int32(px), planes: 1, bitCount: 32}
	bmi.size = uint32(unsafe.Sizeof(bmi))
	var bits unsafe.Pointer
	color32, _, _ := procCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&bmi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if color32 == 0 || bits == nil {
		return 0
	}
	defer procDeleteObject.Call(color32)
	oldBitmap, _, _ := procSelectObject.Call(dc, color32)

	font := createFont(face, -px, fwNormal, antialiasQuality)
	oldFont, _, _ := procSelectObject.Call(dc, font)
	procSetTextColor.Call(dc, 0xFFFFFF)
	procSetBkMode.Call(dc, bkTransparent)
	text := utf16.Encode([]rune{glyph})
	rect := [4]int32{0, 0, int32(px), int32(px)}
	procDrawTextW.Call(dc, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)), uintptr(unsafe.Pointer(&rect)),
		dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	procGdiFlush.Call()
	procSelectObject.Call(dc, oldFont)
	procDeleteObject.Call(font)
	procSelectObject.Call(dc, oldBitmap)

	// Pixels are BGRA; premultiply the colour by each pixel's coverage.
	r, g, b := uint32(color)&0xFF, uint32(color)>>8&0xFF, uint32(color)>>16&0xFF
	pix := unsafe.Slice((*uint32)(bits), px*px)
	for i, p := range pix {
		a := max(p&0xFF, p>>8&0xFF, p>>16&0xFF)
		pix[i] = a<<24 | (r*a/255)<<16 | (g*a/255)<<8 | b*a/255
	}

	mask, _, _ := procCreateBitmap.Call(uintptr(px), uintptr(px), 1, 1, 0)
	defer procDeleteObject.Call(mask)
	info := iconInfo{fIcon: 1, hbmMask: mask, hbmColor: color32}
	icon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&info)))
	return icon
}

// textWidth measures text, without its mnemonic marker, in the dialog's font.
func textWidth(hwnd uintptr, text string) int {
	font, _, _ := procSendMessageW.Call(hwnd, wmGetFont, 0, 0)
	dc, _, _ := procGetDC.Call(hwnd)
	defer procReleaseDC.Call(hwnd, dc)
	old, _, _ := procSelectObject.Call(dc, font)
	defer procSelectObject.Call(dc, old)
	s := utf16.Encode([]rune(strings.ReplaceAll(text, "&", "")))
	if len(s) == 0 {
		return 0
	}
	var size [2]int32
	procGetTextExtentPoint.Call(dc, uintptr(unsafe.Pointer(&s[0])), uintptr(len(s)), uintptr(unsafe.Pointer(&size)))
	return int(size[0])
}

func windowText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf)
}
