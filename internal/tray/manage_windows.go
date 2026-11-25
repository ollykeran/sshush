package tray

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ollykeran/sshush/internal/clipboard"
	"github.com/ollykeran/sshush/internal/platform"
	"github.com/ollykeran/sshush/internal/selftest"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vaultops"
	"github.com/ollykeran/sshush/internal/version"
	"golang.org/x/sys/windows"
)

// The main window is a dialog that runs its own modal loop on a thread of its
// own, so the tray keeps working while it is open. It can be resized: the
// template creates its controls and mainLayout places them. Its list is a
// common-controls ListView. Every operation runs on another goroutine, which
// hands its result back with PostMessage, so a slow agent never freezes the
// window.
var (
	comctl32                 = windows.NewLazySystemDLL("comctl32.dll")
	procInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")
	procPostMessageW         = user32.NewProc("PostMessageW")
	procShowWindow           = user32.NewProc("ShowWindow")
	procGetDlgCtrlID         = user32.NewProc("GetDlgCtrlID")
	procGetFocus             = user32.NewProc("GetFocus")
	procIsWindowEnabled      = user32.NewProc("IsWindowEnabled")
	procLoadImageW           = user32.NewProc("LoadImageW")
	procGetSystemMetrics     = user32.NewProc("GetSystemMetrics")
	procMessageBoxW          = user32.NewProc("MessageBoxW")
	procGetClientRect        = user32.NewProc("GetClientRect")
	procBeginDeferWindowPos  = user32.NewProc("BeginDeferWindowPos")
	procDeferWindowPos       = user32.NewProc("DeferWindowPos")
	procEndDeferWindowPos    = user32.NewProc("EndDeferWindowPos")
	procGetWindowPlacement   = user32.NewProc("GetWindowPlacement")
	procSetWindowPlacement   = user32.NewProc("SetWindowPlacement")
	procCreatePopupMenu      = user32.NewProc("CreatePopupMenu")
	procAppendMenuW          = user32.NewProc("AppendMenuW")
	procTrackPopupMenu       = user32.NewProc("TrackPopupMenu")
	procDestroyMenu          = user32.NewProc("DestroyMenu")
	procClientToScreen       = user32.NewProc("ClientToScreen")
	procGetKeyState          = user32.NewProc("GetKeyState")
	mainDialogProc           = windows.NewCallback(mainWindowProc)
	initCommonControls       sync.Once
)

const (
	wmSize              = 0x0005
	wmGetMinMaxInfo     = 0x0024
	wmContextMenu       = 0x007B
	wmSetIcon           = 0x0080
	wmNotify            = 0x004E
	wmNextDlgCtl        = 0x0028
	wmApp               = 0x8000
	wmMainDone          = wmApp + 1 // an operation finished; take mainWindow.result
	wmMainRefresh       = wmApp + 2 // the tray saw the agent change
	bnClicked           = 0
	bmGetCheck          = 0x00F0
	bmSetCheck          = 0x00F1
	cbnSelChange        = 1
	cbAddString         = 0x0143
	cbGetCurSel         = 0x0147
	cbSetCurSel         = 0x014E
	swHide              = 0
	swShowNormal        = 1
	swMaximize          = 3
	swShow              = 5
	swRestore           = 9
	sizeMinimized       = 1
	iconSmall           = 0
	iconBig             = 1
	imageIcon           = 1
	smCxIcon            = 11
	smCxSmIcon          = 49
	iccListViewClasses  = 0x1
	lvmFirst            = 0x1000
	lvmDeleteAllItems   = lvmFirst + 9
	lvmGetNextItem      = lvmFirst + 12
	lvmGetItemRect      = lvmFirst + 14
	lvmEnsureVisible    = lvmFirst + 19
	lvmGetColumnWidth   = lvmFirst + 29
	lvmSetColumnWidth   = lvmFirst + 30
	lvmSetItemState     = lvmFirst + 43
	lvmSetExtendedStyle = lvmFirst + 54
	lvmInsertItemW      = lvmFirst + 77
	lvmInsertColumnW    = lvmFirst + 97
	lvmSetItemTextW     = lvmFirst + 116
	lvsExFullRowSelect  = 0x20
	lvsExDoubleBuffer   = 0x10000
	lvifText            = 0x1
	lvcfWidth           = 0x2
	lvcfText            = 0x4
	lvniSelected        = 0x2
	lvisFocused         = 0x1
	lvisSelected        = 0x2
	lvirSelectBounds    = 3
	lvscwAutosizeHeader = ^uintptr(1) // -2
	lvnItemChanged      = 0xFFFFFF9B  // LVN_FIRST - 1, as the uint32 NMHDR.code holds it
	lvnKeyDown          = 0xFFFFFF65  // LVN_FIRST - 55
	nmDblClk            = 0xFFFFFFFD  // NM_FIRST - 3
	vkControl           = 0x11
	vkDelete            = 0x2E
	vkF2                = 0x71
	vkF5                = 0x74
	mbYesNo             = 0x4
	mbDefButton2        = 0x100
	idYes               = 6
	mfString            = 0x0000
	mfGrayed            = 0x0001
	mfSeparator         = 0x0800
	tpmRightButton      = 0x0002
	tpmReturnCmd        = 0x0100
	swpNoZOrderActivate = 0x0004 | 0x0010 // NOZORDER, NOACTIVATE
	defaultClientW      = 900             // the first open, at 96 DPI
	defaultClientH      = 600
	placementValue      = "MainWindow"
	windowPlacementSize = 44 // sizeof(WINDOWPLACEMENT)
)

// initCommonControlsEx is INITCOMMONCONTROLSEX.
type initCommonControlsEx struct {
	size, icc uint32
}

// lvColumn is LVCOLUMNW.
type lvColumn struct {
	mask      uint32
	fmt       int32
	cx        int32
	text      *uint16
	textMax   int32
	subItem   int32
	image     int32
	order     int32
	cxMin     int32
	cxDefault int32
	cxIdeal   int32
}

// lvItem is LVITEMW.
type lvItem struct {
	mask      uint32
	item      int32
	subItem   int32
	state     uint32
	stateMask uint32
	text      *uint16
	textMax   int32
	image     int32
	lParam    uintptr
	indent    int32
	groupID   int32
	columns   uint32
	puColumns *uint32
	piColFmt  *int32
	group     int32
}

// nmHdr is NMHDR, and nmLVKeyDown NMLVKEYDOWN.
type nmHdr struct {
	_      uintptr // hwndFrom
	idFrom uintptr
	code   uint32
}

type nmLVKeyDown struct {
	_    nmHdr
	vkey uint16
	_    uint32 // flags
}

// minMaxInfo is MINMAXINFO.
type minMaxInfo struct {
	_            [2]int32 // ptReserved
	_            [2]int32 // ptMaxSize
	_            [2]int32 // ptMaxPosition
	minTrackSize [2]int32
	_            [2]int32 // ptMaxTrackSize
}

// windowPlacement is WINDOWPLACEMENT.
type windowPlacement struct {
	length, flags, showCmd uint32
	minPosition            [2]int32
	maxPosition            [2]int32
	normalPosition         [4]int32
}

// mainWindowHwnd is the open main window, or 0. The tray reads it to bring
// the window forward rather than open a second, and to tell it to refresh.
var mainWindowHwnd atomic.Uintptr

// mainWindowOpen is set from the moment a window is asked for until it closes.
var mainWindowOpen atomic.Bool

// mainWindow is the open window's state. Fields other than mu and result
// belong to the window's thread.
type mainWindow struct {
	t         *tray
	hwnd      uintptr
	state     MainState
	rows      []vaultops.IdentityView // what the list shows, in list order
	detail    DetailView
	busy      bool
	statusErr bool
	tone      Tone // the header's colour
	// lastAgentGlyph keeps the start/stop button's icon while it is disabled.
	lastAgentGlyph rune
	ui             *uiCache
	// frame is how much bigger the window is than its client area.
	frame [2]int32
	// hidden holds the controls show has hidden.
	hidden map[uint16]bool
	// page is the page shown: pageKeys or pageVault.
	page  int
	vault VaultView

	mu     sync.Mutex
	result *opResult
}

// opResult is what an operation hands back to the window's thread.
type opResult struct {
	state  MainState
	status string // what the operation did, "" when it did nothing to report
	err    error
	// report, when set, is shown in a message box with reportFlags.
	report      string
	reportFlags uint32
	// background marks a refresh nobody asked for, after the agent changed:
	// it leaves the last action's message unless the agent's state moved on.
	background bool
}

// The main window's pages.
const (
	pageKeys = iota
	pageVault
)

// pageValue is the registry value that remembers the page last shown.
const pageValue = "MainPage"

// activeMain is the window the dialog procedure serves; there is only one.
var activeMain *mainWindow

// openMain opens the main window, or brings the open one forward. It returns
// at once, so it can run on the tray's own thread.
func (t *tray) openMain() {
	if h := mainWindowHwnd.Load(); h != 0 {
		procShowWindow.Call(h, swRestore)
		procSetForegroundWindow.Call(h)
		return
	}
	if !mainWindowOpen.CompareAndSwap(false, true) {
		return // opening already
	}
	go func() {
		defer mainWindowOpen.Store(false)
		if err := showMainWindow(t); err != nil {
			showError(err.Error())
		}
	}()
}

// notifyMainWindow tells an open main window to look at the agent again.
func notifyMainWindow() {
	if h := mainWindowHwnd.Load(); h != 0 {
		procPostMessageW.Call(h, wmMainRefresh, 0, 0)
	}
}

// showMainWindow runs the window until the user closes it.
func showMainWindow(t *tray) error {
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	initCommonControls.Do(func() {
		icc := initCommonControlsEx{icc: iccListViewClasses}
		icc.size = uint32(unsafe.Sizeof(icc))
		procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
	})

	activeMain = &mainWindow{t: t, hidden: map[uint16]bool{}}
	defer func() {
		if activeMain.ui != nil {
			activeMain.ui.release() // the dialog and its controls are gone
		}
		activeMain = nil
	}()

	r, e := runTemplate(0, mainTemplate(), mainDialogProc)
	mainWindowHwnd.Store(0)
	if r == -1 {
		return fmt.Errorf("could not open the sshush window: %w", e)
	}
	return nil
}

func mainWindowProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	w := activeMain
	if w == nil {
		return 0
	}
	switch msg {
	case wmInitDialog:
		w.hwnd = hwnd
		w.init()
		themeWindow(hwnd)
		mainWindowHwnd.Store(hwnd)
		w.run("Asking the agent…", nil)
		procSetFocus.Call(control(hwnd, idMainList))
		return 0 // focus set here

	case wmSize:
		if wParam != sizeMinimized && w.ui != nil {
			w.layout()
		}
		return 0

	case wmGetMinMaxInfo:
		if w.ui != nil {
			mw, mh := mainMinSize(layoutMetrics{dpi: w.ui.dpi})
			mmi := *(**minMaxInfo)(unsafe.Pointer(&lParam))
			mmi.minTrackSize = [2]int32{int32(mw) + w.frame[0], int32(mh) + w.frame[1]}
		}
		return 0

	case wmCtlColorStatic:
		switch lParam {
		case control(hwnd, idMainAgent):
			if c, ok := toneColor(w.tone, darkNow()); ok {
				return ctlColorText(msg, wParam, c)
			}
		case control(hwnd, idMainSource), control(hwnd, idMainDetailSub), control(hwnd, idMainFpLabel),
			control(hwnd, idMainPubLabel), control(hwnd, idMainFromLabel), control(hwnd, idMainVersion),
			control(hwnd, idVaultSub):
			return ctlColorText(msg, wParam, pick(secondaryText, darkNow()))
		case control(hwnd, idMainFp), control(hwnd, idMainPub):
			return fieldCtlColor(wParam)
		case control(hwnd, idMainStatus):
			return themedCtlColor(msg, wParam, w.statusErr)
		}
		if id, _, _ := procGetDlgCtrlID.Call(lParam); isVaultRowLabel(uint16(id)) {
			return ctlColorText(msg, wParam, pick(secondaryText, darkNow()))
		}
		return themedCtlColor(msg, wParam, false)

	case wmCtlColorDlg, wmCtlColorBtn, wmCtlColorEdit, wmCtlColorListBox:
		return themedCtlColor(msg, wParam, false)

	case wmThemeChanged:
		applyWindowTheme(hwnd)
		// The button icons are drawn in the theme's colours.
		old := w.ui.takeGlyphs()
		w.updateButtons()
		destroyImageLists(old)
		return 1

	case wmDestroy:
		w.savePlacement()
		saveSetting(pageValue, []byte{byte(w.page)})
		unthemeWindow(hwnd)
		return 0

	case wmContextMenu:
		if wParam == control(hwnd, idMainList) {
			w.contextMenu(lParam)
			return 1
		}
		return 0

	case wmNotify:
		hdr := *(**nmHdr)(unsafe.Pointer(&lParam))
		if hdr.idFrom != idMainList {
			return 0
		}
		switch hdr.code {
		case nmCustomDraw:
			return w.customDraw(hwnd, lParam)
		case lvnItemChanged:
			if !w.busy {
				w.updateButtons()
			}
		case lvnKeyDown:
			w.listKey((*(**nmLVKeyDown)(unsafe.Pointer(&lParam))).vkey)
		case nmDblClk:
			w.toggleLoaded()
		}
		return 0

	case wmCommand:
		id, code := wParam&0xFFFF, (wParam>>16)&0xFFFF
		if id == idMainTheme {
			if code == cbnSelChange {
				w.themeSelected()
			}
			return 1
		}
		if code != bnClicked {
			return 0
		}
		if id == idOK { // Enter
			if focus, _, _ := procGetFocus.Call(); focus == control(hwnd, idMainList) {
				w.toggleLoaded()
			}
			return 1
		}
		if w.command(uint16(id)) {
			return 1
		}
		return 0

	case wmMainDone:
		w.finish()
		return 1

	case wmMainRefresh:
		if !w.busy {
			w.run("", nil)
		}
		return 1
	}
	return 0
}

// command runs what the button, or the context menu's item, id stands for:
// the menu's items carry their buttons' IDs.
func (w *mainWindow) command(id uint16) bool {
	switch id {
	case idCancel: // also Esc and the close button
		procEndDialog.Call(w.hwnd, idCancel)
	case idMainAgentToggle:
		w.startOrStop()
	case idMainLock:
		w.lockOrUnlock()
	case idMainCreateVault:
		w.createVault()
	case idMainAdd:
		w.add()
	case idMainCreateKey:
		w.createKey()
	case idMainRemove:
		w.remove()
	case idMainLoad:
		w.toggleLoaded()
	case idMainAutoload:
		w.toggleAutoload()
	case idMainRename:
		w.rename()
	case idMainCopyFp:
		w.copyFingerprint()
	case idMainCopyPub:
		w.copyPublicKey()
	case idMainSelftest:
		w.selftest()
	case idMainRefresh:
		w.run("Asking the agent…", nil)
	case idMainStartup:
		w.startupToggled()
	case idMainTabKeys:
		w.selectPage(pageKeys)
	case idMainTabVault:
		w.selectPage(pageVault)
	case idVaultOpenFolder:
		w.showVaultInExplorer()
	case idVaultCopyPath:
		w.copyVaultPath()
	case idVaultBackup:
		w.backupVault()
	case idVaultImport:
		w.importVault()
	case idVaultMode:
		w.switchMode()
	case idVaultPasswd:
		w.changePassphrase()
	case idVaultRecovery:
		w.newRecoveryPhrase()
	default:
		return false
	}
	return true
}

// init sets the window's icon, fonts, the list's columns, the settings, and
// its size: where it was last closed, else a comfortable default.
func (w *mainWindow) init() {
	w.ui = newUICache(w.hwnd)
	procSendMessageW.Call(control(w.hwnd, idMainAgent), wmSetFont, w.ui.headerFont(), 0)
	procSendMessageW.Call(control(w.hwnd, idMainDetailTitle), wmSetFont, w.ui.titleFont(), 0)
	procSendMessageW.Call(control(w.hwnd, idVaultTitle), wmSetFont, w.ui.titleFont(), 0)
	procSendMessageW.Call(control(w.hwnd, idMainFp), wmSetFont, w.ui.monoFont(), 0)
	procSendMessageW.Call(control(w.hwnd, idMainPub), wmSetFont, w.ui.monoFont(), 0)

	var module windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &module)
	for _, size := range []struct{ which, metric uintptr }{{iconBig, smCxIcon}, {iconSmall, smCxSmIcon}} {
		px, _, _ := procGetSystemMetrics.Call(size.metric)
		// go-winres embeds the program's icon as resource 1.
		if icon, _, _ := procLoadImageW.Call(uintptr(module), 1, imageIcon, px, px, 0); icon != 0 {
			procSendMessageW.Call(w.hwnd, wmSetIcon, size.which, icon)
		}
	}

	list := control(w.hwnd, idMainList)
	style := uintptr(lvsExFullRowSelect | lvsExDoubleBuffer)
	procSendMessageW.Call(list, lvmSetExtendedStyle, style, style)
	for i, name := range mainColumns {
		text, _ := windows.UTF16PtrFromString(name)
		col := lvColumn{mask: lvcfText | lvcfWidth, cx: 80, text: text}
		procSendMessageW.Call(list, lvmInsertColumnW, uintptr(i), uintptr(unsafe.Pointer(&col)))
	}

	combo := control(w.hwnd, idMainTheme)
	for i, c := range themeChoices {
		p, _ := windows.UTF16PtrFromString(c.label)
		procSendMessageW.Call(combo, cbAddString, 0, uintptr(unsafe.Pointer(p)))
		if c.choice == currentThemeChoice() {
			procSendMessageW.Call(combo, cbSetCurSel, uintptr(i), 0)
		}
	}
	procSendMessageW.Call(control(w.hwnd, idMainStartup), bmSetCheck, boolArg(startupEnabled()), 0)
	if b := loadSetting(pageValue); len(b) == 1 && b[0] == pageVault {
		w.page = pageVault
	}
	w.checkTab()
	setText(control(w.hwnd, idMainVersion), "sshush "+version.Version)

	var wr, cr [4]int32
	procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&wr)))
	procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&cr)))
	w.frame = [2]int32{(wr[2] - wr[0]) - cr[2], (wr[3] - wr[1]) - cr[3]}
	if !w.restorePlacement() {
		cw, ch := scale(defaultClientW, w.ui.dpi), scale(defaultClientH, w.ui.dpi)
		ww, wh := int32(cw)+w.frame[0], int32(ch)+w.frame[1]
		// Centred where the template's centring put the smaller window.
		x, y := wr[0]-(ww-(wr[2]-wr[0]))/2, wr[1]-(wh-(wr[3]-wr[1]))/2
		procSetWindowPos.Call(w.hwnd, 0, uintptr(max(x, 0)), uintptr(max(y, 0)), uintptr(ww), uintptr(wh), swpNoZOrderActivate)
	}
	w.updateButtons()
}

// savePlacement remembers where the window is, and its size, for next time.
func (w *mainWindow) savePlacement() {
	wp := windowPlacement{length: windowPlacementSize}
	if r, _, _ := procGetWindowPlacement.Call(w.hwnd, uintptr(unsafe.Pointer(&wp))); r == 0 {
		return
	}
	saveSetting(placementValue, unsafe.Slice((*byte)(unsafe.Pointer(&wp)), windowPlacementSize))
}

// restorePlacement puts the window where it was last closed, reporting
// whether there was such a place. Windows moves it onto a screen if the one
// it was on has gone.
func (w *mainWindow) restorePlacement() bool {
	b := loadSetting(placementValue)
	if len(b) != windowPlacementSize {
		return false
	}
	var wp windowPlacement
	copy(unsafe.Slice((*byte)(unsafe.Pointer(&wp)), windowPlacementSize), b)
	if wp.length != windowPlacementSize {
		return false
	}
	if wp.showCmd != swMaximize { // a maximized window opens maximized, anything else normal
		wp.showCmd = swShowNormal
	}
	r, _, _ := procSetWindowPlacement.Call(w.hwnd, uintptr(unsafe.Pointer(&wp)))
	return r != 0
}

// run marks the window busy, shows working, and runs op, then probes the
// agent, on another goroutine. A nil op only probes. finish takes the result.
func (w *mainWindow) run(working string, op func() opResult) {
	if w.busy {
		return
	}
	w.busy = true
	if working != "" {
		w.setStatus(working, false)
	}
	w.updateButtons()
	hwnd, t := w.hwnd, w.t
	go func() {
		res := opResult{background: op == nil && working == ""}
		if op != nil {
			res = op()
			t.refresh() // the tray shows the change too
		}
		// Read now, not before op: switching what the agent serves changes it.
		res.state = ProbeMain(t.environment())
		w.mu.Lock()
		w.result = &res
		w.mu.Unlock()
		procPostMessageW.Call(hwnd, wmMainDone, 0, 0)
	}()
}

// done is the opResult of an operation that did what it says, or failed.
func done(status string, err error) opResult {
	if err != nil {
		return opResult{err: err}
	}
	return opResult{status: status}
}

// finish shows the result run handed back.
func (w *mainWindow) finish() {
	w.mu.Lock()
	res := w.result
	w.result = nil
	w.mu.Unlock()
	if res == nil {
		return
	}
	keep := ""
	if sel := w.selected(); sel != nil {
		keep = sel.Fingerprint
	}
	before := agentLine(w.state.Reachable, w.state.Managed, w.state.Backend)
	w.state = res.state
	w.fill(keep)
	w.busy = false
	tone, icon := headerOf(w.state)
	w.tone = tone
	procSendMessageW.Call(control(w.hwnd, idMainStateIcon), stmSetIcon, w.ui.stateIcon(icon), 0)
	setText(control(w.hwnd, idMainAgent), agentLine(w.state.Reachable, w.state.Managed, w.state.Backend))
	setText(control(w.hwnd, idMainSource), sourceLine(w.state))

	text, isErr := mainStatus(w.state)
	switch {
	case res.background && !isErr && !w.statusErr && before == agentLine(w.state.Reachable, w.state.Managed, w.state.Backend):
		text = windowText(control(w.hwnd, idMainStatus))
	case res.err != nil:
		text, isErr = oneLine(describeLine(res.err)), true
	case res.status != "" && !isErr:
		text = res.status
	}
	w.setStatus(text, isErr)
	w.updateButtons()
	if res.report != "" {
		messageBoxOwned(w.hwnd, res.report, res.reportFlags)
	}
}

// fill replaces the list's rows, selecting the key with fingerprint keep when
// it is still there, else the first.
func (w *mainWindow) fill(keep string) {
	list := control(w.hwnd, idMainList)
	procSendMessageW.Call(list, lvmDeleteAllItems, 0, 0)
	w.rows = w.state.Rows()
	inVault := w.state.showsVault()
	selected := -1
	for i, id := range w.rows {
		for col, text := range keyRow(id, inVault) {
			p, _ := windows.UTF16PtrFromString(text)
			item := lvItem{mask: lvifText, item: int32(i), subItem: int32(col), text: p}
			if col == 0 {
				procSendMessageW.Call(list, lvmInsertItemW, 0, uintptr(unsafe.Pointer(&item)))
			} else {
				procSendMessageW.Call(list, lvmSetItemTextW, uintptr(i), uintptr(unsafe.Pointer(&item)))
			}
		}
		if keep != "" && id.Fingerprint == keep {
			selected = i
		}
	}
	w.sizeColumns()
	if selected < 0 && len(w.rows) > 0 {
		selected = 0
	}
	if selected >= 0 {
		item := lvItem{state: lvisSelected | lvisFocused, stateMask: lvisSelected | lvisFocused}
		procSendMessageW.Call(list, lvmSetItemState, uintptr(selected), uintptr(unsafe.Pointer(&item)))
		procSendMessageW.Call(list, lvmEnsureVisible, uintptr(selected), 0)
	}
}

// sizeColumns fits the type and status columns to their text and gives the
// name the rest of the list's width.
func (w *mainWindow) sizeColumns() {
	list := control(w.hwnd, idMainList)
	var cr [4]int32
	procGetClientRect.Call(list, uintptr(unsafe.Pointer(&cr)))
	rest := int(cr[2])
	for i := len(mainColumns) - 1; i > 0; i-- {
		procSendMessageW.Call(list, lvmSetColumnWidth, uintptr(i), lvscwAutosizeHeader)
		width, _, _ := procSendMessageW.Call(list, lvmGetColumnWidth, uintptr(i), 0)
		rest -= int(width)
	}
	procSendMessageW.Call(list, lvmSetColumnWidth, 0, uintptr(max(rest, scale(120, w.ui.dpi))))
}

// selected returns the row selected in the list, or nil.
func (w *mainWindow) selected() *vaultops.IdentityView {
	r, _, _ := procSendMessageW.Call(control(w.hwnd, idMainList), lvmGetNextItem, ^uintptr(0), lvniSelected)
	i := int(int32(r))
	if i < 0 || i >= len(w.rows) {
		return nil
	}
	return &w.rows[i]
}

// actions is what the buttons may do now: nothing while an operation runs,
// except copy, which touches only the clipboard.
func (w *mainWindow) actions() MainActions {
	a := mainActions(w.state, w.selected())
	if w.busy {
		return MainActions{Copy: a.Copy}
	}
	return a
}

// detailControls are hidden while no key is selected.
var detailControls = []uint16{idMainFpLabel, idMainFp, idMainCopyFp, idMainPubLabel, idMainPub, idMainCopyPub,
	idMainFromLabel, idMainFrom, idMainLoad, idMainAutoload, idMainRename, idMainRemove}

// updateButtons enables what the state and selection allow, fills the
// details pane for the selected key, and lays the window out again for
// labels that changed.
func (w *mainWindow) updateButtons() {
	a := w.actions()
	enable := func(id int, on bool) {
		procEnableWindow.Call(control(w.hwnd, id), boolArg(on))
		if g, ok := buttonGlyphs[uint16(id)]; ok {
			w.setButtonIcon(id, g, on)
		}
	}
	onKeys := func(id uint16, visible bool) { w.show(id, visible && w.page == pageKeys) }
	onVault := func(id uint16, visible bool) { w.show(id, visible && w.page == pageVault) }
	for _, id := range []uint16{idMainCreateKey, idMainRefresh, idMainList, idMainDetailTitle, idMainDetailSub} {
		onKeys(id, true)
	}

	// With no vault yet there is nothing to add keys to: the Vault page
	// creates one.
	needsVault := w.state.needsVault()
	enable(idMainAdd, a.Add)
	enable(idMainCreateKey, !w.busy)
	onKeys(idMainAdd, !needsVault)
	enable(idMainRemove, a.Remove)
	enable(idMainAutoload, a.Autoload)
	enable(idMainRename, a.Rename)
	enable(idMainCopyFp, a.Copy)
	enable(idMainCopyPub, a.Copy)
	if a.Autoload {
		setText(control(w.hwnd, idMainAutoload), autoloadLabel(a.AutoloadOn))
	}
	if a.Unload {
		setText(control(w.hwnd, idMainLoad), "&Unload")
	} else {
		setText(control(w.hwnd, idMainLoad), "L&oad")
	}
	enable(idMainLoad, a.Load || a.Unload)
	w.setButtonIcon(idMainLoad, loadGlyph(a), a.Load || a.Unload)
	if a.Unlock {
		setText(control(w.hwnd, idMainLock), "Unlo&ck…")
	} else {
		setText(control(w.hwnd, idMainLock), "&Lock")
	}
	if a.Start || a.Stop {
		setText(control(w.hwnd, idMainAgentToggle), agentToggleLabel(a))
	}
	enable(idMainAgentToggle, a.Start || a.Stop)
	if a.Start || a.Stop {
		w.lastAgentGlyph = agentGlyph(a)
	}
	w.setButtonIcon(idMainAgentToggle, w.lastAgentGlyph, a.Start || a.Stop)
	w.show(idMainAgentToggle, w.state.Managed)
	enable(idMainSelftest, !w.busy)
	enable(idMainRefresh, !w.busy)

	w.detail = detailOf(w.state, w.selected())
	setText(control(w.hwnd, idMainDetailTitle), w.detail.Title)
	setText(control(w.hwnd, idMainDetailSub), w.detail.Sub)
	setText(control(w.hwnd, idMainFp), w.detail.Fingerprint)
	setText(control(w.hwnd, idMainPub), w.detail.PublicKey)
	setText(control(w.hwnd, idMainFrom), w.detail.From)
	for _, id := range detailControls {
		onKeys(id, !w.detail.Empty)
	}
	if !w.detail.Empty {
		// Autoload and Remove mean nothing to an agent that is not a vault.
		onKeys(idMainAutoload, w.state.showsVault())
		onKeys(idMainRemove, w.state.showsVault())
	}

	// The Vault page. Create vault and Lock share a place: only one shows.
	va := vaultActions(w.state)
	// Whether a button shows goes by what could be done; whether it is
	// enabled, by what can be done now.
	shown := va
	if w.busy {
		va = VaultActions{CopyPath: va.CopyPath, OpenFolder: va.OpenFolder}
	}
	w.vault = vaultOf(w.state)
	setText(control(w.hwnd, idVaultTitle), w.vault.Title)
	setText(control(w.hwnd, idVaultSub), w.vault.Sub)
	onVault(idVaultTitle, true)
	onVault(idVaultSub, true)
	for i := range vaultRowCount {
		shown := i < len(w.vault.Rows)
		if shown {
			setText(control(w.hwnd, int(vaultRowLabel(i))), w.vault.Rows[i].Label)
			setText(control(w.hwnd, int(vaultRowValue(i))), w.vault.Rows[i].Value)
		}
		onVault(vaultRowLabel(i), shown)
		onVault(vaultRowValue(i), shown)
	}
	enable(idMainCreateVault, va.CreateVault)
	onVault(idMainCreateVault, needsVault)
	if va.Unlock {
		setText(control(w.hwnd, idMainLock), "Unlo&ck…")
	} else {
		setText(control(w.hwnd, idMainLock), "&Lock")
	}
	enable(idMainLock, va.Lock || va.Unlock)
	w.setButtonIcon(idMainLock, lockGlyph(a), va.Lock || va.Unlock)
	onVault(idMainLock, w.state.isVault() && !needsVault)
	enable(idVaultOpenFolder, va.OpenFolder)
	enable(idVaultCopyPath, va.CopyPath)
	onVault(idVaultOpenFolder, true)
	onVault(idVaultCopyPath, true)
	enable(idVaultPasswd, va.ChangePassphrase)
	onVault(idVaultPasswd, shown.ChangePassphrase)
	enable(idVaultRecovery, va.NewRecovery)
	onVault(idVaultRecovery, shown.NewRecovery)
	enable(idVaultBackup, va.Backup)
	onVault(idVaultBackup, w.state.showsVault())
	enable(idVaultImport, va.Import)
	onVault(idVaultImport, shown.Import)
	setText(control(w.hwnd, idVaultMode), modeLabel(shown.UseKeys))
	enable(idVaultMode, va.UseVault || va.UseKeys)
	onVault(idVaultMode, shown.UseVault || shown.UseKeys)
	w.layout()

	// A disabled or hidden control cannot keep the focus; hand it to the
	// list, or on the Vault page to its tab.
	if focus, _, _ := procGetFocus.Call(); focus != 0 {
		on, _, _ := procIsWindowEnabled.Call(focus)
		if id, _, _ := procGetDlgCtrlID.Call(focus); on == 0 || w.hidden[uint16(id)] {
			to := idMainList
			if w.page == pageVault {
				to = idMainTabVault
			}
			procSendMessageW.Call(w.hwnd, wmNextDlgCtl, control(w.hwnd, to), 1)
		}
	}
}

// selectPage shows page, and on the Vault page looks at the agent again, so
// what it says about the vault file is current.
func (w *mainWindow) selectPage(page int) {
	if page == w.page {
		return
	}
	w.page = page
	w.checkTab()
	w.updateButtons()
	if page == pageVault && !w.busy {
		w.run("", nil)
	}
}

// checkTab checks the page tab for the page shown.
func (w *mainWindow) checkTab() {
	procSendMessageW.Call(control(w.hwnd, idMainTabKeys), bmSetCheck, boolArg(w.page == pageKeys), 0)
	procSendMessageW.Call(control(w.hwnd, idMainTabVault), bmSetCheck, boolArg(w.page == pageVault), 0)
}

// isVaultRowLabel reports whether id labels one of the Vault page's facts.
func isVaultRowLabel(id uint16) bool {
	return id >= idVaultRows && id < idVaultRows+2*vaultRowCount && (id-idVaultRows)%2 == 0
}

// layout places every control for the window's current size.
func (w *mainWindow) layout() {
	var cr [4]int32
	procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&cr)))
	m := layoutMetrics{dpi: w.ui.dpi, natural: w.naturalWidths(), hidden: w.hidden}
	// The public key's field is as tall as the key wraps to. The field
	// breaks the line after the key's type, and the rest, one long word,
	// wherever it reaches the edge.
	boxes := mainLayout(int(cr[2]), int(cr[3]), m)
	if pub := boxes[idMainPub]; pub.w > 0 && w.detail.PublicKey != "" {
		field := control(w.hwnd, idMainPub)
		inner := max(pub.w-scale(12, w.ui.dpi), 1)
		_, rest, _ := strings.Cut(w.detail.PublicKey, " ")
		m.pubLines = 1 + (textWidth(field, rest)+inner-1)/inner
		if textWidth(field, w.detail.PublicKey) <= inner {
			m.pubLines = 1
		}
		boxes = mainLayout(int(cr[2]), int(cr[3]), m)
	}
	dwp, _, _ := procBeginDeferWindowPos.Call(uintptr(len(boxes)))
	for id, b := range boxes {
		h := control(w.hwnd, int(id))
		if h == 0 {
			continue
		}
		if dwp != 0 {
			dwp, _, _ = procDeferWindowPos.Call(dwp, h, 0, uintptr(b.x), uintptr(b.y), uintptr(b.w), uintptr(b.h), swpNoZOrderActivate)
		}
		if dwp == 0 {
			procSetWindowPos.Call(h, 0, uintptr(b.x), uintptr(b.y), uintptr(b.w), uintptr(b.h), swpNoZOrderActivate)
		}
	}
	if dwp != 0 {
		procEndDeferWindowPos.Call(dwp)
	}
	w.sizeColumns()
	procRedrawWindow.Call(w.hwnd, 0, 0, rdwInvalidate|rdwErase|rdwAllChildren)
}

// naturalWidths measures each button, and the labels that size to their
// text, in the window's font.
func (w *mainWindow) naturalWidths() map[uint16]int {
	pad, iconW, minW := scale(buttonPad, w.ui.dpi), scale(glyphPixels+glyphGap, w.ui.dpi), scale(buttonMinWidth, w.ui.dpi)
	out := map[uint16]int{}
	for _, id := range []uint16{idMainLock, idMainAgentToggle, idMainAdd, idMainCreateVault, idMainCreateKey, idMainRefresh,
		idMainCopyFp, idMainCopyPub, idMainLoad, idMainAutoload, idMainRename, idMainRemove, idMainSelftest,
		idMainTabKeys, idMainTabVault, idVaultOpenFolder, idVaultCopyPath, idVaultBackup, idVaultImport, idVaultMode, idVaultPasswd, idVaultRecovery} {
		content := textWidth(w.hwnd, windowText(control(w.hwnd, int(id))))
		if hasGlyph(id) {
			content += iconW
		}
		out[id] = max(content+2*pad, minW)
	}
	// A check box's box and the gap after it are about as wide as an icon.
	out[idMainStartup] = textWidth(w.hwnd, windowText(control(w.hwnd, idMainStartup))) + iconW + scale(8, w.ui.dpi)
	out[idMainThemeLabel] = textWidth(w.hwnd, windowText(control(w.hwnd, idMainThemeLabel)))
	out[idMainVersion] = textWidth(w.hwnd, windowText(control(w.hwnd, idMainVersion))) + scale(4, w.ui.dpi)
	return out
}

// setButtonIcon shows glyph on the button id, grey when it is disabled.
func (w *mainWindow) setButtonIcon(id int, glyph rune, enabled bool) {
	if glyph == 0 {
		return
	}
	list := w.ui.glyphImages(glyph, glyphColor(uint16(id), glyph, enabled, darkNow()))
	w.ui.setButtonGlyph(control(w.hwnd, id), list)
}

// customDraw greys the list's rows for keys the agent is not serving.
func (w *mainWindow) customDraw(hwnd, lParam uintptr) uintptr {
	cd := *(**nmLVCustomDraw)(unsafe.Pointer(&lParam))
	switch cd.drawStage {
	case cddsPrePaint:
		return setMsgResult(hwnd, cdrfNotifyItemDrw)
	case cddsItemPrePaint:
		if i := int(cd.itemSpec); i < len(w.rows) && Dimmed(w.rows[i]) {
			cd.clrText = pick(dimText, darkNow())
			return setMsgResult(hwnd, cdrfNewFont)
		}
	}
	return setMsgResult(hwnd, cdrfDoDefault)
}

func (w *mainWindow) setStatus(text string, isErr bool) {
	w.statusErr = isErr
	setText(control(w.hwnd, idMainStatus), text)
	// The colour comes from WM_CTLCOLORSTATIC, so repaint even when the text
	// stays the same.
	procRedrawWindow.Call(control(w.hwnd, idMainStatus), 0, 0, rdwInvalidate|rdwErase)
}

// listKey handles the list's keyboard shortcuts. Enter arrives as idOK and
// the menu key as WM_CONTEXTMENU.
func (w *mainWindow) listKey(vk uint16) {
	ctrl, _, _ := procGetKeyState.Call(vkControl)
	switch {
	case vk == vkDelete:
		w.remove()
	case vk == vkF2:
		w.rename()
	case vk == vkF5:
		w.run("Asking the agent…", nil)
	case vk == 'C' && int16(ctrl) < 0:
		w.copyPublicKey()
	}
}

// contextMenu shows the selected key's actions at the pointer, or for the
// keyboard (pos -1) by the selected row.
func (w *mainWindow) contextMenu(pos uintptr) {
	sel := w.selected()
	if sel == nil {
		return
	}
	a := w.actions()
	list := control(w.hwnd, idMainList)
	var pt [2]int32
	if int32(pos) == -1 {
		r := [4]int32{lvirSelectBounds}
		i, _, _ := procSendMessageW.Call(list, lvmGetNextItem, ^uintptr(0), lvniSelected)
		procSendMessageW.Call(list, lvmGetItemRect, i, uintptr(unsafe.Pointer(&r)))
		pt = [2]int32{r[0] + int32(scale(24, w.ui.dpi)), r[3]}
		procClientToScreen.Call(list, uintptr(unsafe.Pointer(&pt)))
	} else {
		pt = [2]int32{int32(int16(pos)), int32(int16(pos >> 16))}
	}

	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	item := func(id uint16, text string, on bool) {
		flags := uintptr(mfString)
		if !on {
			flags |= mfGrayed
		}
		p, _ := windows.UTF16PtrFromString(text)
		procAppendMenuW.Call(menu, flags, uintptr(id), uintptr(unsafe.Pointer(p)))
	}
	sep := func() { procAppendMenuW.Call(menu, mfSeparator, 0, 0) }
	item(idMainCopyPub, "Copy public key\tCtrl+C", a.Copy)
	item(idMainCopyFp, "Copy fingerprint", a.Copy)
	sep()
	if a.Unload {
		item(idMainLoad, "Unload from the agent\tEnter", true)
	} else {
		item(idMainLoad, "Load into the agent\tEnter", a.Load)
	}
	if w.state.showsVault() {
		item(idMainAutoload, strings.ReplaceAll(autoloadLabel(a.AutoloadOn), "&", ""), a.Autoload)
	}
	item(idMainRename, "Rename…\tF2", a.Rename)
	if w.state.showsVault() {
		sep()
		item(idMainRemove, "Remove from the vault…\tDel", a.Remove)
	}
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd, uintptr(pt[0]), uintptr(pt[1]), 0, w.hwnd, 0)
	if cmd != 0 {
		w.command(uint16(cmd))
	}
}

// The actions. Each checks it is allowed now, since a key press or double
// click can ask for one whose button is disabled.

func (w *mainWindow) startOrStop() {
	a := w.actions()
	t := w.t
	switch {
	case a.Start:
		w.run("Starting the agent…", func() opResult {
			if err := StartAgent(t.environment().SocketPath); err != nil {
				return opResult{err: fmt.Errorf("could not start the agent: %w", err)}
			}
			// As 'sshush start' does, ask for the passphrase straight away.
			if s := Probe(t.environment().SocketPath); s.Err == nil && s.Backend.Mode == "vault" && s.Backend.VaultLocked {
				if err := t.promptUnlock(); err != nil && !errors.Is(err, errCancelled) {
					return opResult{err: err}
				}
			}
			return opResult{status: "Started the agent."}
		})
	case a.Stop:
		w.run("Stopping the agent…", func() opResult {
			if err := StopAgent(); err != nil {
				return opResult{err: fmt.Errorf("could not stop the agent: %w", err)}
			}
			return opResult{status: "Stopped the agent. Its keys have left memory."}
		})
	}
}

func (w *mainWindow) lockOrUnlock() {
	a := w.actions()
	t := w.t
	switch {
	case a.Unlock:
		w.run("Waiting for the passphrase…", func() opResult {
			err := t.promptUnlock()
			if errors.Is(err, errCancelled) {
				return opResult{}
			}
			return done("Unlocked the vault.", err)
		})
	case a.Lock:
		w.run("Locking…", func() opResult {
			return done("Locked the vault. Its keys have left the agent.", vaultops.Lock(t.environment()))
		})
	}
}

// createVault asks for a passphrase, twice, creates the vault with a
// recovery phrase, shows the phrase once, and starts the agent on the new
// vault, unlocked.
func (w *mainWindow) createVault() {
	if !w.actions().CreateVault {
		return
	}
	hwnd, t := w.hwnd, w.t
	w.run("Creating the vault…", func() opResult {
		pass, err := askNewPassphrase(hwnd)
		if errors.Is(err, errCancelled) {
			return opResult{}
		}
		if err != nil {
			return opResult{err: err}
		}
		defer clear(pass)
		res, err := vaultops.Init(t.environment(), pass, vaultops.InitOptions{Recovery: true})
		if err != nil {
			return opResult{err: err}
		}
		showRecoveryPhrase(hwnd, recoveryMessage(res.RecoveryFile), res.Mnemonic)
		// The agent was waiting for this vault: start it, and unlock the
		// vault with the passphrase just chosen rather than ask again.
		if Probe(t.environment().SocketPath).Err != nil && !t.environment().AgentUnmanaged {
			if err := StartAgent(t.environment().SocketPath); err != nil {
				return opResult{err: fmt.Errorf("created the vault, but could not start the agent: %w", err)}
			}
		}
		if s := Probe(t.environment().SocketPath); s.Err == nil && s.Backend.Mode == "vault" && s.Backend.VaultLocked {
			if err := vaultops.UnlockPassphrase(t.environment(), pass); err != nil {
				return opResult{err: fmt.Errorf("created the vault, but could not unlock it: %w", err)}
			}
		}
		return opResult{status: "Created and unlocked the vault. Add keys to it to use them."}
	})
}

func (w *mainWindow) add() {
	if !w.actions().Add {
		return
	}
	socketPath, into := w.t.environment().SocketPath, "the agent"
	if w.state.isVault() {
		into = "the vault"
	}
	w.run("Adding keys…", func() opResult {
		paths, err := pickKeyFiles()
		if errors.Is(err, errCancelled) {
			return opResult{}
		}
		if err != nil {
			return opResult{err: err}
		}
		added, err := AddKeys(socketPath, paths, askKeyPassphrase)
		if err != nil && added > 0 {
			err = fmt.Errorf("added %s, but %w", plural(added, "key"), err)
		}
		return done(fmt.Sprintf("Added %s to %s.", plural(added, "key"), into), err)
	})
}

// createKey makes a new key pair, as 'sshush create' does, and adds it to
// the agent when the user leaves that box ticked.
func (w *mainWindow) createKey() {
	if w.busy {
		return
	}
	hwnd, socketPath, canAdd := w.hwnd, w.t.environment().SocketPath, w.actions().Add
	addLabel := "&Add it to the agent"
	if w.state.isVault() {
		addLabel = "&Add it to the vault"
	}
	w.run("Creating a key…", func() opResult {
		path, add, err := promptCreateKey(hwnd, addLabel)
		if errors.Is(err, errCancelled) {
			return opResult{}
		}
		if err != nil {
			return opResult{err: err}
		}
		made := "Created " + utils.DisplayPath(path) + " and its .pub"
		switch {
		case !add:
			return opResult{status: made + "."}
		case !canAdd:
			return opResult{status: made + ". Start or unlock the agent, then add it."}
		}
		if _, err := AddKeys(socketPath, []string{path}, nil); err != nil {
			return opResult{err: fmt.Errorf("created %s, but could not add it: %w", utils.DisplayPath(path), err)}
		}
		return opResult{status: made + ", and added it."}
	})
}

func (w *mainWindow) remove() {
	sel := w.selected()
	if !w.actions().Remove || sel == nil {
		return
	}
	id := *sel
	text := "Remove " + identityName(id) + " from the vault?\n\n" + id.Fingerprint +
		"\n\nThe vault's copy of the private key is deleted and cannot be recovered. " +
		"The key file it was added from, if any, is left alone."
	if messageBoxOwned(w.hwnd, text, mbYesNo|mbIconWarning|mbDefButton2) != idYes {
		return
	}
	env := w.t.environment()
	w.run("Removing "+identityName(id)+"…", func() opResult {
		_, err := vaultops.Remove(env, []string{id.Fingerprint})
		return done("Removed "+identityName(id)+" from the vault.", err)
	})
}

// toggleLoaded loads the selected key into the running agent, or unloads it;
// neither deletes anything.
func (w *mainWindow) toggleLoaded() {
	sel := w.selected()
	a := w.actions()
	if sel == nil || !(a.Load || a.Unload) {
		return
	}
	id, env, inVault := *sel, w.t.environment(), w.state.isVault()
	if a.Load {
		w.run("Loading "+identityName(id)+"…", func() opResult {
			_, err := vaultops.SessionLoad(env, []string{id.Fingerprint})
			return done("Loaded "+identityName(id)+" until the agent stops.", err)
		})
		return
	}
	kept := "; it stays in the vault."
	if !inVault {
		kept = "; its key file is untouched."
	}
	w.run("Unloading "+identityName(id)+"…", func() opResult {
		return done("Unloaded "+identityName(id)+kept, UnloadKey(env.SocketPath, id.Fingerprint))
	})
}

func (w *mainWindow) toggleAutoload() {
	sel := w.selected()
	a := w.actions()
	if sel == nil || !a.Autoload {
		return
	}
	id, env, on := *sel, w.t.environment(), a.AutoloadOn
	w.run("Saving…", func() opResult {
		status := identityName(id) + " will stay unloaded until you load it."
		if on {
			status = identityName(id) + " will load whenever the vault is unlocked."
		}
		_, err := vaultops.SetAutoload(env, []string{id.Fingerprint}, on)
		return done(status, err)
	})
}

// rename asks for a new comment for the selected key and saves it.
func (w *mainWindow) rename() {
	sel := w.selected()
	if sel == nil || !w.actions().Rename {
		return
	}
	id, env, hwnd, keyPaths := *sel, w.t.environment(), w.hwnd, w.t.keyPaths
	w.run("Renaming…", func() opResult {
		spec := inputSpec{
			title:   "Rename key",
			message: "A key's name is its comment: what ssh-add -l shows, and what follows the key in authorized_keys.",
			initial: id.Comment,
			okLabel: "Rename",
		}
		for {
			b, button, err := promptInput(hwnd, spec)
			if err != nil {
				return opResult{err: err}
			}
			if button != idOK {
				return opResult{}
			}
			comment := strings.TrimSpace(string(b))
			status, err := RenameKey(env, keyPaths, id, comment)
			var invalid *invalidCommentError
			if errors.As(err, &invalid) {
				spec.initial, spec.errText = comment, invalid.Error()
				continue
			}
			return done(status, err)
		}
	})
}

func (w *mainWindow) copyFingerprint() {
	if w.actions().Copy {
		w.copied(w.detail.Fingerprint, "fingerprint")
	}
}

func (w *mainWindow) copyPublicKey() {
	if w.actions().Copy {
		w.copied(w.detail.PublicKey, "public key")
	}
}

func (w *mainWindow) copied(text, what string) {
	if err := clipboard.Copy(text); err != nil {
		w.setStatus("Could not copy the "+what+": "+err.Error(), true)
		return
	}
	w.setStatus("Copied the "+what+" of "+w.detail.Title+".", false)
}

// showVaultInExplorer opens the vault's folder with the file selected, or
// with no file yet the nearest folder there is.
func (w *mainWindow) showVaultInExplorer() {
	path := w.vault.Path
	if path == "" {
		return
	}
	args := `/select,"` + path + `"`
	if _, err := os.Stat(path); err != nil {
		dir := filepath.Dir(path)
		for {
			if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				w.setStatus("The vault's folder does not exist yet.", false)
				return
			}
			dir = parent
		}
		args = `"` + dir + `"`
	}
	verb, _ := windows.UTF16PtrFromString("open")
	exe, _ := windows.UTF16PtrFromString("explorer.exe")
	params, _ := windows.UTF16PtrFromString(args)
	if err := windows.ShellExecute(windows.Handle(w.hwnd), verb, exe, params, nil, windows.SW_SHOWNORMAL); err != nil {
		w.setStatus("Could not open Explorer: "+err.Error(), true)
	}
}

func (w *mainWindow) copyVaultPath() {
	if w.vault.Path == "" {
		return
	}
	if err := clipboard.Copy(w.vault.Path); err != nil {
		w.setStatus("Could not copy the vault's path: "+err.Error(), true)
		return
	}
	w.setStatus("Copied the vault's path.", false)
}

// selftest runs the same checks as 'sshush selftest' and shows the report.
func (w *mainWindow) selftest() {
	socketPath := w.t.environment().SocketPath
	w.run("Running the selftest…", func() opResult {
		// The tray's own SSH_AUTH_SOCK is whatever it inherited; what matters
		// is the value new terminals, and so ssh, will get.
		persisted, _ := platform.PersistedEnv("SSH_AUTH_SOCK")
		report := selftest.Run(socketPath, persisted, persisted)
		return opResult{report: selftestText(report), reportFlags: selftestIcon(report.Worst())}
	})
}

func (w *mainWindow) themeSelected() {
	r, _, _ := procSendMessageW.Call(control(w.hwnd, idMainTheme), cbGetCurSel, 0, 0)
	if i := int(int32(r)); i >= 0 && i < len(themeChoices) {
		setThemeChoice(themeChoices[i].choice)
	}
}

func (w *mainWindow) startupToggled() {
	box := control(w.hwnd, idMainStartup)
	on, _, _ := procSendMessageW.Call(box, bmGetCheck, 0, 0)
	if err := setStartup(on == 1); err != nil {
		procSendMessageW.Call(box, bmSetCheck, boolArg(on != 1), 0)
		w.setStatus("Could not change whether sshush starts at sign-in: "+err.Error(), true)
		if errors.Is(err, errStartupInSettings) {
			openStartupSettings()
		}
		return
	}
	if on == 1 {
		w.setStatus("sshush will start in the notification area when you sign in.", false)
	} else {
		w.setStatus("sshush will no longer start when you sign in.", false)
	}
}

// oneLine joins a multi-line message, such as several joined errors, for the
// one-line status.
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", "; ")), " ")
}

func setText(hwnd uintptr, text string) {
	if windowText(hwnd) == text {
		return // setting it again would repaint, and flicker
	}
	p, _ := windows.UTF16PtrFromString(text)
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(p)))
}

// show shows or hides the control id and remembers which, for the layout:
// Windows calls every control hidden until the window itself is shown.
func (w *mainWindow) show(id uint16, visible bool) {
	h := control(w.hwnd, int(id))
	if visible {
		procShowWindow.Call(h, swShow)
		delete(w.hidden, id)
	} else {
		procShowWindow.Call(h, swHide)
		w.hidden[id] = true
	}
}

// messageBoxOwned asks from the main window, which it disables until answered.
func messageBoxOwned(owner uintptr, text string, flags uint32) int {
	title, _ := windows.UTF16PtrFromString("sshush")
	body, _ := windows.UTF16PtrFromString(text)
	r, _, _ := procMessageBoxW.Call(owner, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), uintptr(flags))
	return int(r)
}
