package tray

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// The page tabs are owner-drawn buttons, drawn as Windows 11's own top
// navigation is (Settings, Edge, the Store): a plain label, the page shown
// in the normal text colour with a bar in the accent colour under it, the
// others in the secondary colour, and a line under them all. Windows' tab
// control would look like tabs too, but takes no dark theme.

var (
	procFillRect      = user32.NewProc("FillRect")
	procDrawFocusRect = user32.NewProc("DrawFocusRect")
	procDwmGetColor   = dwmapi.NewProc("DwmGetColorizationColor")
)

const (
	wmDrawItem     = 0x002B
	odsFocus       = 0x0010
	odsNoFocusRect = 0x0200
	dtHidePrefix   = 0x00100000
	colorHighlight = 13
	colorBtnText   = 18
	tabBarPixels   = 3 // the selected tab's bar, at 96 DPI
	tabBarInsetPx  = 6 // how far the bar stops short of the label's ends
)

// drawItem is DRAWITEMSTRUCT.
type drawItem struct {
	_         uint32 // CtlType
	ctlID     uint32
	_         uint32 // itemID
	_         uint32 // itemAction
	itemState uint32
	hwndItem  uintptr
	hdc       uintptr
	rc        [4]int32
	_         uintptr // itemData
}

// tabPage is the page a tab button shows, or -1 when id is not a tab.
func tabPage(id uint16) int {
	switch id {
	case idMainTabKeys:
		return pageKeys
	case idMainTabVault:
		return pageVault
	case idMainTabSettings:
		return pageSettings
	}
	return -1
}

// accentColor is the user's Windows accent colour, made lighter in the dark
// theme so the bar stands out as Windows' own does.
func accentColor(dark bool) colorRef {
	var argb uint32
	var opaque int32
	c := sysColor(colorHighlight)
	if r, _, _ := procDwmGetColor.Call(uintptr(unsafe.Pointer(&argb)), uintptr(unsafe.Pointer(&opaque))); r == 0 {
		c = rgb(uint8(argb>>16), uint8(argb>>8), uint8(argb))
	}
	if dark {
		mix := func(v uint32) uint8 { return uint8((v + 2*0xFF) / 3) }
		c = rgb(mix(uint32(c)&0xFF), mix(uint32(c)>>8&0xFF), mix(uint32(c)>>16&0xFF))
	}
	return c
}

// drawTab draws the tab button described by lParam's DRAWITEMSTRUCT, and
// reports whether it was one.
func (w *mainWindow) drawTab(lParam uintptr) bool {
	di := *(**drawItem)(unsafe.Pointer(&lParam))
	page := tabPage(uint16(di.ctlID))
	if page < 0 {
		return false
	}
	dark, selected := darkNow(), page == w.page
	hdc, rc := di.hdc, di.rc

	// The window's background, as a label's.
	bg := ctlColorText(wmCtlColorStatic, hdc, 0)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), bg)

	text := windowText(di.hwndItem)
	p, _ := windows.UTF16FromString(text)
	font, _, _ := procSendMessageW.Call(di.hwndItem, wmGetFont, 0, 0)
	old, _, _ := procSelectObject.Call(hdc, font)
	defer procSelectObject.Call(hdc, old)
	color := pick(secondaryText, dark)
	if selected {
		color = sysColor(colorBtnText)
		if dark {
			color = darkText
		}
	}
	procSetTextColor.Call(hdc, uintptr(color))
	procSetBkMode.Call(hdc, bkTransparent)
	flags := uintptr(dtCenter | dtVCenter | dtSingleLine)
	if di.itemState&odsNoFocusRect != 0 {
		flags |= dtHidePrefix // Windows hides the access keys until Alt is pressed
	}
	label := rc
	label[3] -= int32(scale(tabBarPixels, w.ui.dpi)) // the bar's room
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&p[0])), ^uintptr(0), uintptr(unsafe.Pointer(&label)), flags)

	if selected {
		inset := int32(scale(tabBarInsetPx, w.ui.dpi))
		bar := [4]int32{rc[0] + inset, rc[3] - int32(scale(tabBarPixels, w.ui.dpi)), rc[2] - inset, rc[3]}
		brush, _, _ := procCreateSolidBrush.Call(uintptr(accentColor(dark)))
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&bar)), brush)
		procDeleteObject.Call(brush)
	}
	if di.itemState&odsFocus != 0 && di.itemState&odsNoFocusRect == 0 {
		focus := rc
		focus[0], focus[1], focus[2], focus[3] = focus[0]+2, focus[1]+2, focus[2]-2, focus[3]-2
		procDrawFocusRect.Call(hdc, uintptr(unsafe.Pointer(&focus)))
	}
	return true
}

// The line under the tabs' brushes, made when first drawn, on the window's
// thread.
var lightDivider, darkDivider uintptr

// tabDividerBrush is the brush for the line under the tabs: Windows' own
// divider colour for the theme.
func tabDividerBrush() uintptr {
	if darkNow() {
		if darkDivider == 0 {
			darkDivider, _, _ = procCreateSolidBrush.Call(uintptr(rgb(0x3A, 0x3A, 0x3A)))
		}
		return darkDivider
	}
	if lightDivider == 0 {
		lightDivider, _, _ = procCreateSolidBrush.Call(uintptr(rgb(0xE0, 0xE0, 0xE0)))
	}
	return lightDivider
}
