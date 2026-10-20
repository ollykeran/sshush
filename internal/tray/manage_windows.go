package tray

import (
	"errors"
	"fmt"
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ollykeran/sshush/internal/platform"
	"github.com/ollykeran/sshush/internal/selftest"
	"github.com/ollykeran/sshush/internal/vaultops"
	"golang.org/x/sys/windows"
)

// The main window is a dialog that runs its own modal loop on a thread of its
// own, so the tray keeps working while it is open. Its list is a
// common-controls ListView. Every operation runs on another goroutine, which
// hands its result back with PostMessage, so a slow agent never freezes the
// window.
var (
	comctl32                 = windows.NewLazySystemDLL("comctl32.dll")
	procInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")
	procPostMessageW         = user32.NewProc("PostMessageW")
	procShowWindow           = user32.NewProc("ShowWindow")
	procGetFocus             = user32.NewProc("GetFocus")
	procIsWindowEnabled      = user32.NewProc("IsWindowEnabled")
	procLoadImageW           = user32.NewProc("LoadImageW")
	procGetSystemMetrics     = user32.NewProc("GetSystemMetrics")
	procMessageBoxW          = user32.NewProc("MessageBoxW")
	mainDialogProc           = windows.NewCallback(mainWindowProc)
	initCommonControls       sync.Once
)

const (
	wmSetIcon           = 0x0080
	wmNotify            = 0x004E
	wmNextDlgCtl        = 0x0028
	wmApp               = 0x8000
	wmMainDone          = wmApp + 1 // an operation finished; take mainWindow.result
	wmMainRefresh       = wmApp + 2 // the tray saw the agent change
	bnClicked           = 0
	cbnSelChange        = 1
	cbAddString         = 0x0143
	cbGetCurSel         = 0x0147
	cbSetCurSel         = 0x014E
	swRestore           = 9
	iconSmall           = 0
	iconBig             = 1
	imageIcon           = 1
	smCxIcon            = 11
	smCxSmIcon          = 49
	iccListViewClasses  = 0x1
	lvmFirst            = 0x1000
	lvmDeleteAllItems   = lvmFirst + 9
	lvmGetNextItem      = lvmFirst + 12
	lvmEnsureVisible    = lvmFirst + 19
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
	lvscwAutosizeHeader = ^uintptr(1) // -2
	lvnItemChanged      = 0xFFFFFF9B  // LVN_FIRST - 1, as the uint32 NMHDR.code holds it
	lvnKeyDown          = 0xFFFFFF65  // LVN_FIRST - 55
	nmDblClk            = 0xFFFFFFFD  // NM_FIRST - 3
	vkDelete            = 0x2E
	mbYesNo             = 0x4
	mbDefButton2        = 0x100
	idYes               = 6
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
	hwndFrom uintptr
	idFrom   uintptr
	code     uint32
}

type nmLVKeyDown struct {
	hdr   nmHdr
	vkey  uint16
	flags uint32
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
	busy      bool
	statusErr bool
	tone      Tone // the header's colour
	// lastAgentGlyph keeps the start/stop button's icon while it is disabled.
	lastAgentGlyph rune
	ui             *uiCache

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
}

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

	activeMain = &mainWindow{t: t}
	defer func() {
		if activeMain.ui != nil {
			activeMain.ui.release() // the dialog and its controls are gone
		}
		activeMain = nil
	}()

	words := mainTemplate()
	tmpl := make([]uint32, (len(words)+1)/2) // DWORD-aligned, as for the passphrase dialog
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&tmpl[0])), len(words)), words)
	var module windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &module)
	r, _, e := procDialogBoxIndirectParamW.Call(uintptr(module), uintptr(unsafe.Pointer(&tmpl[0])), 0, mainDialogProc, 0)
	goruntime.KeepAlive(tmpl)
	mainWindowHwnd.Store(0)
	if int32(r) == -1 {
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
		return 1 // the dialog manager focuses the first tab stop

	case wmCtlColorStatic:
		switch lParam {
		case control(hwnd, idMainAgent):
			if c, ok := toneColor(w.tone, darkNow()); ok {
				return ctlColorText(msg, wParam, c)
			}
		case control(hwnd, idMainSource):
			return ctlColorText(msg, wParam, pick(secondaryText, darkNow()))
		case control(hwnd, idMainStatus):
			return themedCtlColor(msg, wParam, w.statusErr)
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
		unthemeWindow(hwnd)
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
			w.updateButtons()
		case lvnKeyDown:
			if key := *(**nmLVKeyDown)(unsafe.Pointer(&lParam)); key.vkey == vkDelete {
				w.remove()
			}
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
		switch id {
		case idCancel: // also Esc and the close button
			procEndDialog.Call(hwnd, idCancel)
		case idMainAgentToggle:
			w.startOrStop()
		case idMainLock:
			w.lockOrUnlock()
		case idMainAdd:
			w.add()
		case idMainRemove:
			w.remove()
		case idMainLoad, idMainUnload:
			w.toggleLoaded()
		case idMainAutoload:
			w.toggleAutoload()
		case idMainSelftest:
			w.selftest()
		case idMainRefresh:
			w.run("Asking the agent…", nil)
		default:
			return 0
		}
		return 1

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

// init sets the window's icon, the list's columns and the theme choices.
func (w *mainWindow) init() {
	w.ui = newUICache(w.hwnd)
	procSendMessageW.Call(control(w.hwnd, idMainAgent), wmSetFont, w.ui.headerFont(), 0)

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
	w.autosizeColumns()

	combo := control(w.hwnd, idMainTheme)
	for i, c := range themeChoices {
		p, _ := windows.UTF16PtrFromString(c.label)
		procSendMessageW.Call(combo, cbAddString, 0, uintptr(unsafe.Pointer(p)))
		if c.choice == currentThemeChoice() {
			procSendMessageW.Call(combo, cbSetCurSel, uintptr(i), 0)
		}
	}
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
	hwnd, env := w.hwnd, w.t.env
	go func() {
		var res opResult
		if op != nil {
			res = op()
			w.t.refresh() // the tray shows the change too
		}
		res.state = ProbeMain(env)
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
	w.busy = false
	keep := ""
	if sel := w.selected(); sel != nil {
		keep = sel.Fingerprint
	}
	w.state = res.state
	w.fill(keep)
	tone, icon := headerOf(w.state)
	w.tone = tone
	procSendMessageW.Call(control(w.hwnd, idMainStateIcon), stmSetIcon, w.ui.stateIcon(icon), 0)
	setText(control(w.hwnd, idMainAgent), agentLine(w.state.Reachable, w.state.Managed, w.state.Backend))
	setText(control(w.hwnd, idMainSource), sourceLine(w.state))

	text, isErr := mainStatus(w.state)
	switch {
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
	w.autosizeColumns()
	if selected < 0 && len(w.rows) > 0 {
		selected = 0
	}
	if selected >= 0 {
		item := lvItem{state: lvisSelected | lvisFocused, stateMask: lvisSelected | lvisFocused}
		procSendMessageW.Call(list, lvmSetItemState, uintptr(selected), uintptr(unsafe.Pointer(&item)))
		procSendMessageW.Call(list, lvmEnsureVisible, uintptr(selected), 0)
	}
}

func (w *mainWindow) autosizeColumns() {
	list := control(w.hwnd, idMainList)
	for i := range mainColumns {
		procSendMessageW.Call(list, lvmSetColumnWidth, uintptr(i), lvscwAutosizeHeader)
	}
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

// actions is what the buttons may do now: nothing while an operation runs.
func (w *mainWindow) actions() MainActions {
	if w.busy {
		return MainActions{}
	}
	return mainActions(w.state, w.selected())
}

// updateButtons enables what the state and selection allow; while an
// operation runs, only Close.
func (w *mainWindow) updateButtons() {
	a := w.actions()
	enable := func(id int, on bool) {
		procEnableWindow.Call(control(w.hwnd, id), boolArg(on))
		if g, ok := buttonGlyphs[uint16(id)]; ok {
			w.setButtonIcon(id, g, on)
		}
	}
	enable(idMainAdd, a.Add)
	enable(idMainRemove, a.Remove)
	enable(idMainLoad, a.Load)
	enable(idMainUnload, a.Unload)
	enable(idMainAutoload, a.Autoload)
	if a.Autoload {
		setText(control(w.hwnd, idMainAutoload), autoloadLabel(a.AutoloadOn))
	}
	if a.Unlock {
		setText(control(w.hwnd, idMainLock), "Unlo&ck…")
	} else {
		setText(control(w.hwnd, idMainLock), "&Lock")
	}
	enable(idMainLock, a.Lock || a.Unlock)
	w.setButtonIcon(idMainLock, lockGlyph(a), a.Lock || a.Unlock)
	if a.Start || a.Stop {
		setText(control(w.hwnd, idMainAgentToggle), agentToggleLabel(a))
	}
	enable(idMainAgentToggle, a.Start || a.Stop)
	if a.Start || a.Stop {
		w.lastAgentGlyph = agentGlyph(a)
	}
	w.setButtonIcon(idMainAgentToggle, w.lastAgentGlyph, a.Start || a.Stop)
	enable(idMainSelftest, !w.busy)
	enable(idMainRefresh, !w.busy)
	w.ui.fitRows(w.hwnd, func(id uint16) bool {
		_, ok := buttonGlyphs[id]
		return ok || id == idMainLock || id == idMainAgentToggle
	})

	// A disabled control cannot keep the focus; hand it to the list.
	if focus, _, _ := procGetFocus.Call(); focus != 0 {
		if on, _, _ := procIsWindowEnabled.Call(focus); on == 0 {
			procSendMessageW.Call(w.hwnd, wmNextDlgCtl, control(w.hwnd, idMainList), 1)
		}
	}
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
}

// The actions. Each checks it is allowed now, since a key press or double
// click can ask for one whose button is disabled.

func (w *mainWindow) startOrStop() {
	a := w.actions()
	t := w.t
	switch {
	case a.Start:
		w.run("Starting the agent…", func() opResult {
			if err := StartAgent(t.env.SocketPath); err != nil {
				return opResult{err: fmt.Errorf("could not start the agent: %w", err)}
			}
			// As 'sshush start' does, ask for the passphrase straight away.
			if s := Probe(t.env.SocketPath); s.Err == nil && s.Backend.Mode == "vault" && s.Backend.VaultLocked {
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
			return done("Locked the vault. Its keys have left the agent.", vaultops.Lock(t.env))
		})
	}
}

func (w *mainWindow) add() {
	if !w.actions().Add {
		return
	}
	socketPath, into := w.t.env.SocketPath, "the agent"
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
	env := w.t.env
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
	id, env, inVault := *sel, w.t.env, w.state.isVault()
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
	id, env, on := *sel, w.t.env, a.AutoloadOn
	w.run("Saving…", func() opResult {
		status := identityName(id) + " will stay unloaded until you load it."
		if on {
			status = identityName(id) + " will load whenever the vault is unlocked."
		}
		_, err := vaultops.SetAutoload(env, []string{id.Fingerprint}, on)
		return done(status, err)
	})
}

// selftest runs the same checks as 'sshush selftest' and shows the report.
func (w *mainWindow) selftest() {
	socketPath := w.t.env.SocketPath
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

// identityName names a key in a sentence by its comment, else its type.
func identityName(id vaultops.IdentityView) string {
	if id.Comment != "" {
		return id.Comment
	}
	return id.KeyType + " key"
}

// oneLine joins a multi-line message, such as several joined errors, for the
// one-line status.
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", "; ")), " ")
}

func setText(hwnd uintptr, text string) {
	p, _ := windows.UTF16PtrFromString(text)
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(p)))
}

// messageBoxOwned asks from the main window, which it disables until answered.
func messageBoxOwned(owner uintptr, text string, flags uint32) int {
	title, _ := windows.UTF16PtrFromString("sshush")
	body, _ := windows.UTF16PtrFromString(text)
	r, _, _ := procMessageBoxW.Call(owner, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), uintptr(flags))
	return int(r)
}
