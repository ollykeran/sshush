package tray

import (
	goruntime "runtime"
	"time"
	"unsafe"

	"github.com/ollykeran/sshush/internal/vaultops"
	"golang.org/x/sys/windows"
)

// Windows tells a window when the session locks and when the PC is about to
// sleep, so the tray keeps one, hidden, on a thread of its own. It is an
// ordinary top-level window rather than a message-only one: those do not get
// WM_POWERBROADCAST. Time without input is read with GetLastInputInfo.

var (
	wtsapi32                           = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSRegisterSessionNotification = wtsapi32.NewProc("WTSRegisterSessionNotification")
	procRegisterClassExW               = user32.NewProc("RegisterClassExW")
	procCreateWindowExW                = user32.NewProc("CreateWindowExW")
	procDefWindowProcW                 = user32.NewProc("DefWindowProcW")
	procGetMessageW                    = user32.NewProc("GetMessageW")
	procTranslateMessage               = user32.NewProc("TranslateMessage")
	procDispatchMessageW               = user32.NewProc("DispatchMessageW")
	procGetLastInputInfo               = user32.NewProc("GetLastInputInfo")
	procGetTickCount                   = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetTickCount")
)

const (
	wmWTSSessionChange   = 0x02B1
	wmPowerBroadcast     = 0x0218
	wtsConsoleDisconnect = 0x2 // switched to another user
	wtsRemoteDisconnect  = 0x4
	wtsSessionLock       = 0x7
	pbtAPMSuspend        = 0x4
	notifyForThisSession = 0
	// idleCheckEvery is how often time without input is looked at: the
	// lock comes at most this much after the limit.
	idleCheckEvery = 20 * time.Second
)

// wndClassEx is WNDCLASSEXW.
type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   windows.Handle
	icon       windows.Handle
	cursor     windows.Handle
	background windows.Handle
	menuName   *uint16
	className  *uint16
	iconSm     windows.Handle
}

// msg is MSG.
type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      [2]int32
	private uint32
}

// lastInputInfo is LASTINPUTINFO.
type lastInputInfo struct {
	size uint32
	time uint32
}

// autoLocker is the tray's watcher, reached from the window procedure.
var autoLocker *tray

// lockPolicyValue is the registry value the policy is kept in.
const lockPolicyValue = "LockPolicy"

// lockPolicy is the tray's current auto-lock policy.
func (t *tray) lockPolicy() LockPolicy {
	t.policyMu.Lock()
	defer t.policyMu.Unlock()
	return t.policy
}

// setLockPolicy changes the policy and remembers it.
func (t *tray) setLockPolicy(p LockPolicy) {
	t.policyMu.Lock()
	t.policy = p
	t.policyMu.Unlock()
	saveSetting(lockPolicyValue, p.encode())
}

// watchForLeaving loads the policy and starts watching the session, power
// and input for the moments it locks on.
func (t *tray) watchForLeaving() {
	t.policy = decodeLockPolicy(loadSetting(lockPolicyValue))
	autoLocker = t
	go runAutoLockWindow()
	go func() {
		var w idleWatch
		for range time.Tick(idleCheckEvery) {
			if w.check(idleTime(), t.lockPolicy().idleLimit()) {
				t.autoLock()
			}
		}
	}()
}

// idleTime is how long since the last keyboard or mouse input in this session.
func idleTime() time.Duration {
	info := lastInputInfo{size: uint32(unsafe.Sizeof(lastInputInfo{}))}
	if r, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		return 0
	}
	now, _, _ := procGetTickCount.Call()
	// Both are milliseconds since boot, in 32 bits; the subtraction wraps
	// correctly across the 49.7-day rollover.
	return time.Duration(uint32(now)-info.time) * time.Millisecond
}

// runAutoLockWindow makes the hidden window and runs its message loop, for
// the life of the tray.
func runAutoLockWindow() {
	goruntime.LockOSThread()
	className, _ := windows.UTF16PtrFromString("sshush-autolock")
	wc := wndClassEx{wndProc: windows.NewCallback(autoLockProc), className: className}
	wc.size = uint32(unsafe.Sizeof(wc))
	if r, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return
	}
	hwnd, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	if hwnd == 0 {
		return
	}
	procWTSRegisterSessionNotification.Call(hwnd, notifyForThisSession)
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func autoLockProc(hwnd, message, wParam, lParam uintptr) uintptr {
	t := autoLocker
	switch message {
	case wmWTSSessionChange:
		switch wParam {
		case wtsSessionLock, wtsConsoleDisconnect, wtsRemoteDisconnect:
			if t != nil && t.lockPolicy().locksOn(EventSessionLock) {
				go t.autoLock()
			}
		}
		return 0
	case wmPowerBroadcast:
		// Locked before returning: Windows sleeps soon after this message
		// is answered, and a lock started on another goroutine might not
		// finish first.
		if wParam == pbtAPMSuspend && t != nil && t.lockPolicy().locksOn(EventSuspend) {
			t.autoLock()
		}
		return 1
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, message, wParam, lParam)
	return r
}

// autoLock locks the vault if a vault agent is serving it unlocked.
func (t *tray) autoLock() {
	env := t.environment()
	s := Probe(env.SocketPath)
	if s.Err != nil || s.Backend.Mode != "vault" || s.Backend.VaultLocked {
		return
	}
	if vaultops.Lock(env) == nil {
		t.refresh()
		notifyMainWindow()
	}
}

// initAutoLock fills the Vault page's Auto-lock settings from the policy.
func (w *mainWindow) initAutoLock() {
	setText(control(w.hwnd, idAutoLockTitle), "Auto-lock")
	p := w.t.lockPolicy()
	procSendMessageW.Call(control(w.hwnd, idAutoLockSession), bmSetCheck, boolArg(p.OnSessionLock), 0)
	procSendMessageW.Call(control(w.hwnd, idAutoLockSleep), bmSetCheck, boolArg(p.OnSuspend), 0)
	combo := control(w.hwnd, idAutoLockIdle)
	for _, m := range idleChoices {
		text, _ := windows.UTF16PtrFromString(idleLabel(m))
		procSendMessageW.Call(combo, cbAddString, 0, uintptr(unsafe.Pointer(text)))
	}
	procSendMessageW.Call(combo, cbSetCurSel, uintptr(idleIndex(p.IdleMinutes)), 0)
}

// autoLockChanged takes the policy from the Vault page's settings.
func (w *mainWindow) autoLockChanged() {
	checked := func(id int) bool {
		r, _, _ := procSendMessageW.Call(control(w.hwnd, id), bmGetCheck, 0, 0)
		return r == 1
	}
	p := LockPolicy{OnSessionLock: checked(idAutoLockSession), OnSuspend: checked(idAutoLockSleep)}
	if r, _, _ := procSendMessageW.Call(control(w.hwnd, idAutoLockIdle), cbGetCurSel, 0, 0); int(int32(r)) >= 0 && int(r) < len(idleChoices) {
		p.IdleMinutes = idleChoices[r]
	}
	w.t.setLockPolicy(p)
	w.setStatus(autoLockStatus(p), false)
}
