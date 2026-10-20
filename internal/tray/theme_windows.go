package tray

import (
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Light and dark drawing for the tray's menu and windows.
//
// The title bar goes dark through a documented DWM attribute. Win32 controls
// have no documented dark mode, so this does what Windows' own Win32 programs
// (Explorer, Notepad, Task Manager) do: give each control the "DarkMode_"
// visual style uxtheme ships, set the app mode that makes menus and
// scrollbars follow, and paint dialog backgrounds and text through the
// WM_CTLCOLOR* messages. The app-mode calls are exported only by ordinal and
// are looked up, not linked, so a Windows without them simply stays light.
// MessageBox has no dark mode at all, so its boxes stay light.
var (
	uxtheme                         = windows.NewLazySystemDLL("uxtheme.dll")
	dwmapi                          = windows.NewLazySystemDLL("dwmapi.dll")
	procSetWindowTheme              = uxtheme.NewProc("SetWindowTheme")
	procDwmSetWindowAttribute       = dwmapi.NewProc("DwmSetWindowAttribute")
	procEnumChildWindows            = user32.NewProc("EnumChildWindows")
	procGetClassNameW               = user32.NewProc("GetClassNameW")
	procRedrawWindow                = user32.NewProc("RedrawWindow")
	procSetWindowPos                = user32.NewProc("SetWindowPos")
	procSystemParametersInfoW       = user32.NewProc("SystemParametersInfoW")
	procGetSysColor                 = user32.NewProc("GetSysColor")
	procCreateSolidBrush            = gdi32.NewProc("CreateSolidBrush")
	procSetBkColor                  = gdi32.NewProc("SetBkColor")
	childThemeCallback              = windows.NewCallback(themeChild)
	darkWindowBrush, darkFieldBrush uintptr
	brushesOnce                     sync.Once
)

// uxtheme's unnamed exports, by ordinal (Windows 10 1903 and later).
const (
	ordRefreshImmersiveColorPolicyState = 104
	ordAllowDarkModeForWindow           = 133
	ordSetPreferredAppMode              = 135
	ordFlushMenuThemes                  = 136
)

// PreferredAppMode values for SetPreferredAppMode.
const (
	appModeAllowDark  = 1
	appModeForceDark  = 2
	appModeForceLight = 3
)

const (
	wmThemeChanged       = wmApp + 3 // re-read the theme and repaint
	wmCtlColorEdit       = 0x0133
	wmCtlColorListBox    = 0x0134
	wmThemeChangedSys    = 0x031A // WM_THEMECHANGED
	wmCtlColorBtn        = 0x0135
	wmCtlColorDlg        = 0x0136
	wmDestroy            = 0x0002
	lvmSetBkColor        = lvmFirst + 1
	lvmGetHeader         = lvmFirst + 31
	lvmSetTextColor      = lvmFirst + 36
	lvmSetTextBkColor    = lvmFirst + 38
	dwmwaDarkMode        = 20 // DWMWA_USE_IMMERSIVE_DARK_MODE
	dwmwaDarkModeOld     = 19 // its number before Windows 10 20H1
	spiGetHighContrast   = 0x0042
	hcfHighContrastOn    = 0x1
	colorWindow          = 5
	colorWindowText      = 8
	rdwInvalidate        = 0x0001
	rdwErase             = 0x0004
	rdwAllChildren       = 0x0080
	rdwFrame             = 0x0400
	swpFrameChanged      = 0x0020
	swpRepaintFlags      = 0x0001 | 0x0002 | 0x0004 | 0x0010 // NOSIZE, NOMOVE, NOZORDER, NOACTIVATE
	personalizeKey       = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`
	appsUseLightTheme    = "AppsUseLightTheme"
	settingsKey          = `Software\sshush\Tray`
	settingsThemeValue   = "Theme"
	regNotifyLastSet     = 0x00000004
	regNotifySubtreeOnly = false
)

var (
	themeChoice  atomic.Value // ThemeChoice
	uxOrdinals   struct{ refresh, allowWindow, setMode, flush uintptr }
	uxLoadOnce   sync.Once
	themedHwnds  sync.Map // hwnd -> struct{}: open windows to repaint on a change
	themeWatched sync.Once
)

// initTheme reads the saved choice, sets the app mode, and starts watching
// Windows' setting. Call it before the menu is first shown.
func initTheme() {
	themeChoice.Store(loadThemeChoice())
	applyAppMode()
	themeWatched.Do(func() { go watchSystemTheme() })
}

func currentThemeChoice() ThemeChoice {
	if c, ok := themeChoice.Load().(ThemeChoice); ok {
		return c
	}
	return ThemeSystem
}

// darkNow reports whether the tray draws dark right now.
func darkNow() bool { return currentThemeChoice().Dark(systemDark(), highContrast()) }

// setThemeChoice saves c and redraws the menu and every open window with it.
func setThemeChoice(c ThemeChoice) {
	themeChoice.Store(c)
	if k, _, err := registry.CreateKey(registry.CURRENT_USER, settingsKey, registry.SET_VALUE); err == nil {
		_ = k.SetStringValue(settingsThemeValue, string(c))
		k.Close()
	}
	applyAppMode()
	broadcastTheme()
}

func loadThemeChoice() ThemeChoice {
	k, err := registry.OpenKey(registry.CURRENT_USER, settingsKey, registry.QUERY_VALUE)
	if err != nil {
		return ThemeSystem
	}
	defer k.Close()
	s, _, _ := k.GetStringValue(settingsThemeValue)
	return ParseThemeChoice(s)
}

// systemDark reads Windows' "Choose your app mode" setting.
func systemDark() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, personalizeKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	light, _, err := k.GetIntegerValue(appsUseLightTheme)
	return err == nil && light == 0
}

func highContrast() bool {
	var hc struct {
		size, flags uint32
		scheme      *uint16
	}
	hc.size = uint32(unsafe.Sizeof(hc))
	r, _, _ := procSystemParametersInfoW.Call(spiGetHighContrast, uintptr(hc.size), uintptr(unsafe.Pointer(&hc)), 0)
	return r != 0 && hc.flags&hcfHighContrastOn != 0
}

// watchSystemTheme repaints whenever Windows' personalisation settings
// change, which is where both the app mode and high contrast's effect show.
func watchSystemTheme() {
	k, err := registry.OpenKey(registry.CURRENT_USER, personalizeKey, registry.NOTIFY)
	if err != nil {
		return
	}
	defer k.Close()
	for {
		if windows.RegNotifyChangeKeyValue(windows.Handle(k), regNotifySubtreeOnly, regNotifyLastSet, 0, false) != nil {
			return
		}
		ux := uxProcs()
		if ux.refresh != 0 {
			syscallN(ux.refresh)
		}
		applyAppMode()
		broadcastTheme()
	}
}

func uxProcs() *struct{ refresh, allowWindow, setMode, flush uintptr } {
	uxLoadOnce.Do(func() {
		if uxtheme.Load() != nil {
			return
		}
		h := windows.Handle(uxtheme.Handle())
		get := func(ord uintptr) uintptr { p, _ := windows.GetProcAddressByOrdinal(h, ord); return p }
		// SetPreferredAppMode arrived in 1903; without it, none of these are
		// the functions this expects.
		if uxOrdinals.setMode = get(ordSetPreferredAppMode); uxOrdinals.setMode == 0 {
			return
		}
		uxOrdinals.refresh = get(ordRefreshImmersiveColorPolicyState)
		uxOrdinals.allowWindow = get(ordAllowDarkModeForWindow)
		uxOrdinals.flush = get(ordFlushMenuThemes)
	})
	return &uxOrdinals
}

func syscallN(proc uintptr, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(proc, args...)
	return r
}

// applyAppMode sets the process's app mode, which decides how menus and
// themed scrollbars draw, and makes open menus pick it up.
func applyAppMode() {
	ux := uxProcs()
	if ux.setMode == 0 {
		return
	}
	mode := uintptr(appModeAllowDark) // follow the system
	switch currentThemeChoice() {
	case ThemeLight:
		mode = appModeForceLight
	case ThemeDark:
		mode = appModeForceDark
	}
	if highContrast() {
		mode = appModeForceLight
	}
	syscallN(ux.setMode, mode)
	if ux.flush != 0 {
		syscallN(ux.flush)
	}
}

// themeWindow draws hwnd, a dialog, in the current theme and keeps it in step
// with later changes until untheme.
func themeWindow(hwnd uintptr) {
	themedHwnds.Store(hwnd, struct{}{})
	applyWindowTheme(hwnd)
}

func unthemeWindow(hwnd uintptr) { themedHwnds.Delete(hwnd) }

// broadcastTheme asks every themed window to redraw. They do it on their own
// threads, which own them.
func broadcastTheme() {
	themedHwnds.Range(func(k, _ any) bool {
		procPostMessageW.Call(k.(uintptr), wmThemeChanged, 0, 0)
		return true
	})
}

// applyWindowTheme draws hwnd and its controls in the current theme. It runs
// on the window's thread.
func applyWindowTheme(hwnd uintptr) {
	dark := darkNow()
	setDarkTitleBar(hwnd, dark)
	allowDark(hwnd, dark)
	procEnumChildWindows.Call(hwnd, childThemeCallback, boolArg(dark))
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, swpRepaintFlags|swpFrameChanged)
	procRedrawWindow.Call(hwnd, 0, 0, rdwInvalidate|rdwErase|rdwAllChildren|rdwFrame)
}

func setDarkTitleBar(hwnd uintptr, dark bool) {
	on := uint32(boolArg(dark))
	for _, attr := range []uintptr{dwmwaDarkMode, dwmwaDarkModeOld} {
		if r, _, _ := procDwmSetWindowAttribute.Call(hwnd, attr, uintptr(unsafe.Pointer(&on)), unsafe.Sizeof(on)); r == 0 {
			return
		}
	}
}

func allowDark(hwnd uintptr, dark bool) {
	if ux := uxProcs(); ux.allowWindow != 0 {
		syscallN(ux.allowWindow, hwnd, boolArg(dark))
	}
}

// themeChild gives one control the visual style for the theme; lParam is 1
// for dark. It is an EnumChildWindows callback.
func themeChild(child, lParam uintptr) uintptr {
	dark := lParam != 0
	allowDark(child, dark)
	var buf [64]uint16
	n, _, _ := procGetClassNameW.Call(child, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	switch windows.UTF16ToString(buf[:n]) {
	case "Button":
		// The Explorer style draws dark in a window allowed dark mode. Only
		// WM_THEMECHANGED makes a button reload it: setting a dark style by
		// name instead leaves a button that has been light drawing its
		// disabled label in the enabled colour.
		setWindowTheme(child, "Explorer")
		procSendMessageW.Call(child, wmThemeChangedSys, 0, 0)
	case "Edit", "ComboBox":
		setWindowTheme(child, darkOr(dark, "DarkMode_CFD", ""))
	case classListView:
		setWindowTheme(child, darkOr(dark, "DarkMode_Explorer", "Explorer"))
		if header, _, _ := procSendMessageW.Call(child, lvmGetHeader, 0, 0); header != 0 {
			allowDark(header, dark)
			setWindowTheme(header, darkOr(dark, "DarkMode_ItemsView", ""))
		}
		bk, text := sysColor(colorWindow), sysColor(colorWindowText)
		if dark {
			bk, text = darkField, darkText
		}
		procSendMessageW.Call(child, lvmSetBkColor, 0, uintptr(bk))
		procSendMessageW.Call(child, lvmSetTextBkColor, 0, uintptr(bk))
		procSendMessageW.Call(child, lvmSetTextColor, 0, uintptr(text))
	}
	return 1 // keep enumerating
}

func darkOr(dark bool, darkName, lightName string) string {
	if dark {
		return darkName
	}
	return lightName
}

// setWindowTheme applies the named visual style, or with "" restores the
// default.
func setWindowTheme(hwnd uintptr, name string) {
	if name == "" {
		procSetWindowTheme.Call(hwnd, 0, 0)
		return
	}
	p, _ := windows.UTF16PtrFromString(name)
	procSetWindowTheme.Call(hwnd, uintptr(unsafe.Pointer(p)), 0)
}

func sysColor(index uintptr) colorRef {
	c, _, _ := procGetSysColor.Call(index)
	return colorRef(c)
}

// themedCtlColor answers a WM_CTLCOLOR* message, msg, for a dialog: it sets
// the colours on hdc and returns the background brush, or 0 to let Windows
// draw. isErr draws the text as an error.
func themedCtlColor(msg, hdc uintptr, isErr bool) uintptr {
	if isErr {
		return ctlColorText(msg, hdc, pick([2]colorRef{lightErrorText, darkErrorText}, darkNow()))
	}
	if !darkNow() {
		return 0
	}
	return ctlColorText(msg, hdc, darkText)
}

// ctlColorText is themedCtlColor drawing the text in text.
func ctlColorText(msg, hdc uintptr, text colorRef) uintptr {
	procSetTextColor.Call(hdc, uintptr(text))
	if !darkNow() {
		procSetBkMode.Call(hdc, bkTransparent)
		brush, _, _ := procGetSysColorBrush.Call(colorBtnFace)
		return brush
	}
	brushesOnce.Do(func() {
		darkWindowBrush, _, _ = procCreateSolidBrush.Call(uintptr(darkWindow))
		darkFieldBrush, _, _ = procCreateSolidBrush.Call(uintptr(darkField))
	})
	if msg == wmCtlColorEdit || msg == wmCtlColorListBox {
		procSetBkColor.Call(hdc, uintptr(darkField))
		return darkFieldBrush
	}
	procSetBkColor.Call(hdc, uintptr(darkWindow))
	return darkWindowBrush
}
