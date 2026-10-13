package clipboard

import (
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"github.com/atotto/clipboard"
	"golang.org/x/sys/windows"
)

// copyText goes through the clipboard API rather than piping to clip.exe, which
// reads its input in the console's code page and mangles anything not ASCII —
// a key comment with an accent in it, say.
func copyText(text string) error {
	return clipboard.WriteAll(text)
}

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procOpenClipboard           = user32.NewProc("OpenClipboard")
	procCloseClipboard          = user32.NewProc("CloseClipboard")
	procEmptyClipboard          = user32.NewProc("EmptyClipboard")
	procSetClipboardData        = user32.NewProc("SetClipboardData")
	procRegisterClipboardFormat = user32.NewProc("RegisterClipboardFormatW")

	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalFree   = kernel32.NewProc("GlobalFree")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	procMoveMemory   = kernel32.NewProc("RtlMoveMemory")
)

const (
	cfUnicodeText = 13

	// The clipboard only takes memory it can move, and GMEM_ZEROINIT is how
	// the DWORD 0 the history formats want gets written without touching it.
	gmemMoveable = 0x0002
	gmemZeroInit = 0x0040
)

// The clipboard formats by which a program tells Windows what may be done with
// what it copied; password managers set the same three. The first is the
// older one, honoured by clipboard history and by third-party clipboard
// managers, and counts by being present at all. The other two are read as a
// DWORD, where 0 means no: not into the Win+V history, not up to the cloud
// clipboard that syncs between devices.
const (
	formatExcludeFromMonitors = "ExcludeClipboardContentFromMonitorProcessing"
	formatCanIncludeInHistory = "CanIncludeInClipboardHistory"
	formatCanUploadToCloud    = "CanUploadToCloudClipboard"
)

// errOpenClipboard marks a failure to get hold of the clipboard at all, as
// opposed to a failure to put something on it: a session with no clipboard
// (a service, a headless runner) or another program sitting on it.
var errOpenClipboard = errors.New("clipboard: cannot open the clipboard")

// copySecret cannot use atotto/clipboard, which sets the text and nothing
// else: the exclusion formats have to go on in the same open-to-close as the
// text, because the history and the sync read the clipboard when it is closed.
func copySecret(text string) error {
	// NUL-terminated UTF-16, which is what CF_UNICODETEXT is. A NUL inside the
	// text would cut it short on paste, so that is refused rather than copied.
	utf16, err := windows.UTF16FromString(text)
	if err != nil {
		return fmt.Errorf("clipboard: %w", err)
	}

	// The clipboard is open per thread: the thread that opens it is the one
	// that has to fill and close it, so the goroutine must not move meanwhile.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := openClipboard(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()

	if r, _, err := procEmptyClipboard.Call(); r == 0 {
		return fmt.Errorf("clipboard: empty: %w", err)
	}
	if err := setSecret(utf16); err != nil {
		// Whatever made it on goes again: the text without its exclusion
		// marks is exactly what this function exists to avoid leaving there.
		procEmptyClipboard.Call()
		return err
	}
	return nil
}

// openClipboard opens the clipboard, which only one program can have open at a
// time. Clipboard managers and remote-desktop clients open it briefly whenever
// it changes, so a refusal is usually over a few milliseconds later; one that
// lasts a second is somebody stuck, or no clipboard to open.
func openClipboard() error {
	deadline := time.Now().Add(time.Second)
	for {
		r, _, err := procOpenClipboard.Call(0)
		if r != 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: %w", errOpenClipboard, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// setSecret fills the open, empty clipboard. The marks go on before the text
// so that there is no point at which the text is there without them.
func setSecret(utf16 []uint16) error {
	exclude, err := registerFormat(formatExcludeFromMonitors)
	if err != nil {
		return err
	}
	history, err := registerFormat(formatCanIncludeInHistory)
	if err != nil {
		return err
	}
	cloud, err := registerFormat(formatCanUploadToCloud)
	if err != nil {
		return err
	}
	var zero [4]byte // a DWORD 0; for the first format any content will do
	for _, format := range []uintptr{exclude, history, cloud} {
		if err := setData(format, unsafe.Pointer(&zero[0]), unsafe.Sizeof(zero)); err != nil {
			return err
		}
	}
	return setData(cfUnicodeText, unsafe.Pointer(&utf16[0]), uintptr(len(utf16))*unsafe.Sizeof(utf16[0]))
}

// registerFormat returns the number Windows knows the named clipboard format
// by, the same one for every program in the session that asks for that name.
func registerFormat(name string) (uintptr, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	r, _, err := procRegisterClipboardFormat.Call(uintptr(unsafe.Pointer(p)))
	if r == 0 {
		return 0, fmt.Errorf("clipboard: register format %s: %w", name, err)
	}
	return r, nil
}

// setData puts a copy of the size bytes at data on the clipboard as format.
// The copy lives in memory from GlobalAlloc, which is what the clipboard
// takes; once SetClipboardData has accepted it the system owns it and frees
// it, so it is only freed here on the paths where that never happened.
func setData(format uintptr, data unsafe.Pointer, size uintptr) error {
	h, _, err := procGlobalAlloc.Call(gmemMoveable|gmemZeroInit, size)
	if h == 0 {
		return fmt.Errorf("clipboard: allocate: %w", err)
	}
	// The locked address is only ever handed back to the system as a number:
	// it is not Go memory, and is never turned into a Go pointer.
	p, _, err := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("clipboard: lock: %w", err)
	}
	procMoveMemory.Call(p, uintptr(data), size)
	procGlobalUnlock.Call(h)

	if r, _, err := procSetClipboardData.Call(format, h); r == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("clipboard: set data: %w", err)
	}
	return nil
}
