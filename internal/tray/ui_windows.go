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
	procMapWindowPoints      = user32.NewProc("MapWindowPoints")
	procImageListCreate      = comctl32.NewProc("ImageList_Create")
	procImageListReplaceIcon = comctl32.NewProc("ImageList_ReplaceIcon")
	procImageListDestroy     = comctl32.NewProc("ImageList_Destroy")
	iconFaceOnce             sync.Once
	iconFace                 string
	iconFaceCandidates       = []string{"Segoe Fluent Icons", "Segoe MDL2 Assets"}
	glyphPixels, statePixels = 16, 32 // at 96 DPI
	headerPoints             = 15
)

const (
	wmSetFont         = 0x0030
	bcmSetImageList   = 0x1602
	ilcColor32        = 0x0020
	buttonImageLeft   = 0
	glyphGap          = 4  // between a button's icon and its text, at 96 DPI
	buttonPad         = 14 // either side of a button's content, at 96 DPI
	buttonMinWidth    = 76
	rowGap            = 6
	wmGetFont         = 0x0031
	swpNoZOrderMove   = 0x0004 | 0x0010 // NOZORDER, NOACTIVATE
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
	rc        [4]int32
	itemSpec  uintptr
	itemState uint32
	itemParam uintptr
	clrText   colorRef
	clrTextBk colorRef
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

func scale(px, dpi int) int { return (px*dpi + 48) / 96 }

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
	if u.header != 0 {
		procDeleteObject.Call(u.header)
		u.header = 0
	}
}

func createFont(face string, height, weight, quality int) uintptr {
	name, _ := windows.UTF16PtrFromString(face)
	f, _, _ := procCreateFontW.Call(uintptr(int32(height)), 0, 0, 0, uintptr(weight), 0, 0, 0,
		defaultCharset, 0, 0, uintptr(quality), 0, uintptr(unsafe.Pointer(name)))
	return f
}

// findIconFace returns the first icon font Windows has, or "". A font that
// is not installed is silently substituted, so each candidate is asked for
// and the face Windows actually chose compared with it.
func findIconFace() string {
	iconFaceOnce.Do(func() {
		dc, _, _ := procCreateCompatibleDC.Call(0)
		defer procDeleteDC.Call(dc)
		for _, face := range iconFaceCandidates {
			f := createFont(face, -16, fwNormal, antialiasQuality)
			old, _, _ := procSelectObject.Call(dc, f)
			var buf [64]uint16
			n, _, _ := procGetTextFaceW.Call(dc, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
			procSelectObject.Call(dc, old)
			procDeleteObject.Call(f)
			if n > 0 && windows.UTF16ToString(buf[:]) == face {
				iconFace = face
				return
			}
		}
	})
	return iconFace
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

// rectIn returns child's rectangle in parent's client coordinates.
func rectIn(parent, child uintptr) (r [4]int32) {
	procGetWindowRect.Call(child, uintptr(unsafe.Pointer(&r)))
	procMapWindowPoints.Call(0, parent, uintptr(unsafe.Pointer(&r)), 2)
	return r
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

// fitRows sizes each button in buttonRows to its icon and label and lays
// each row out again. hasIcon reports whether a button shows an icon.
func (u *uiCache) fitRows(dlg uintptr, hasIcon func(id uint16) bool) {
	for _, row := range buttonRows {
		type placed struct {
			h     uintptr
			r     [4]int32
			width int32
		}
		items := make([]placed, len(row.ids))
		for i, id := range row.ids {
			h := control(dlg, int(id))
			r := rectIn(dlg, h)
			width := r[2] - r[0]
			switch id {
			case idMainTheme: // keeps its width
			case idMainThemeLabel:
				width = int32(textWidth(dlg, windowText(h)))
			default:
				content := textWidth(dlg, windowText(h))
				if hasIcon(id) {
					content += scale(glyphPixels+glyphGap, u.dpi)
				}
				width = int32(max(content+2*scale(buttonPad, u.dpi), scale(buttonMinWidth, u.dpi)))
			}
			items[i] = placed{h, r, width}
		}
		gap := int32(scale(rowGap, u.dpi))
		x := items[0].r[0]
		if row.right {
			x = items[len(items)-1].r[2]
			for _, it := range items {
				x -= it.width
			}
			x -= gap * int32(len(items)-1)
		}
		for _, it := range items {
			if it.r[0] != x || it.r[2]-it.r[0] != it.width {
				procSetWindowPos.Call(it.h, 0, uintptr(x), uintptr(it.r[1]), uintptr(it.width), uintptr(it.r[3]-it.r[1]), swpNoZOrderMove)
			}
			x += it.width + gap
		}
	}
}
