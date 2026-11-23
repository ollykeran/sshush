package tray

import (
	"fmt"
	goruntime "runtime"
	"sync"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"github.com/ollykeran/sshush/internal/clipboard"
	"golang.org/x/sys/windows"
)

// The unlock prompt is an ordinary modal dialog built from an in-memory
// template (see dialog_template.go) and driven by Windows' dialog manager,
// reached through user32 by syscall so no cgo is needed. The manifest
// embedded in sshush-tray.exe gives it themed controls and crisp text at any
// DPI.
var (
	user32                      = windows.NewLazySystemDLL("user32.dll")
	gdi32                       = windows.NewLazySystemDLL("gdi32.dll")
	procDialogBoxIndirectParamW = user32.NewProc("DialogBoxIndirectParamW")
	procEndDialog               = user32.NewProc("EndDialog")
	procGetDlgItem              = user32.NewProc("GetDlgItem")
	procSendMessageW            = user32.NewProc("SendMessageW")
	procSetFocus                = user32.NewProc("SetFocus")
	procSetForegroundWindow     = user32.NewProc("SetForegroundWindow")
	procEnableWindow            = user32.NewProc("EnableWindow")
	procGetWindowTextLengthW    = user32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW          = user32.NewProc("GetWindowTextW")
	procSetWindowTextW          = user32.NewProc("SetWindowTextW")
	procGetSysColorBrush        = user32.NewProc("GetSysColorBrush")
	procMessageBeep             = user32.NewProc("MessageBeep")
	procSetTextColor            = gdi32.NewProc("SetTextColor")
	procSetBkMode               = gdi32.NewProc("SetBkMode")
)

const (
	wmInitDialog       = 0x0110
	wmCommand          = 0x0111
	wmCtlColorStatic   = 0x0138
	emSetLimitText     = 0x00C5
	emSetSel           = 0x00B1
	enChange           = 0x0300
	colorBtnFace       = 15
	bkTransparent      = 1
	mbIconExclamation  = 0x30
	maxPassphraseChars = 1024
)

// dialog holds what the dialog procedure needs. Only one prompt is open at a
// time, and the procedure runs on the thread that opened it, so it is guarded
// for the length of a prompt rather than per access.
var dialog struct {
	sync.Mutex
	spec   inputSpec
	owner  uintptr
	result []byte
}

// Dialog procedures are created once: Windows callbacks are a limited resource.
var (
	dialogProc       = windows.NewCallback(inputDialogProc)
	phraseDialogProc = windows.NewCallback(phraseProc)
)

// promptPassphrase shows the unlock dialog and returns what was typed as
// UTF-8. wrongLast makes the dialog say the previous attempt was wrong. The
// caller owns and zeroes the returned bytes.
func promptPassphrase(message string, wrongLast bool) ([]byte, error) {
	spec := inputSpec{title: "sshush", message: message, okLabel: "Unlock", masked: true}
	if wrongLast {
		spec.errText = "Wrong passphrase. Try again."
	}
	pass, button, err := promptInput(0, spec)
	if err == nil && button != idOK {
		clear(pass)
		return nil, errCancelled
	}
	return pass, err
}

// promptInput shows an input dialog owned by owner, which may be 0, and
// returns what was typed as UTF-8 and the button that closed it: idOK,
// idExtra or idCancel. The caller owns and zeroes the returned bytes.
func promptInput(owner uintptr, spec inputSpec) ([]byte, int, error) {
	dialog.Lock()
	defer dialog.Unlock()
	dialog.spec, dialog.owner, dialog.result = spec, owner, nil

	// The dialog's message loop runs on this thread, and its procedure is
	// called back on it.
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()

	r, e := runTemplate(owner, inputTemplate(spec), dialogProc)
	switch r {
	case idOK, idExtra:
		pass := dialog.result
		dialog.result = nil
		return pass, r, nil
	case idCancel:
		return nil, idCancel, nil
	default:
		return nil, 0, fmt.Errorf("tray: input dialog: %w", e)
	}
}

// runTemplate runs a modal dialog from template words and returns what it
// ended with, or -1 and why it could not open.
func runTemplate(owner uintptr, words []uint16, proc uintptr) (int, error) {
	// DLGTEMPLATE must be DWORD-aligned; a []uint32 backing guarantees it.
	tmpl := make([]uint32, (len(words)+1)/2)
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&tmpl[0])), len(words)), words)
	var module windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &module)
	r, _, e := procDialogBoxIndirectParamW.Call(uintptr(module), uintptr(unsafe.Pointer(&tmpl[0])), owner, proc, 0)
	goruntime.KeepAlive(tmpl)
	return int(int32(r)), e
}

func inputDialogProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmInitDialog:
		edit := control(hwnd, idEdit)
		procSendMessageW.Call(edit, emSetLimitText, maxPassphraseChars, 0)
		if dialog.spec.initial != "" {
			setText(edit, dialog.spec.initial)
			procSendMessageW.Call(edit, emSetSel, 0, ^uintptr(0)) // all of it
		}
		enableOK(hwnd)
		themeWindow(hwnd)
		procSetForegroundWindow.Call(hwnd)
		procSetFocus.Call(edit)
		if dialog.spec.errText != "" {
			procMessageBeep.Call(mbIconExclamation)
		}
		return 0 // focus set here, not by the dialog manager

	case wmCtlColorStatic, wmCtlColorDlg, wmCtlColorBtn, wmCtlColorEdit:
		return themedCtlColor(msg, wParam, msg == wmCtlColorStatic && lParam == control(hwnd, idError))

	case wmThemeChanged:
		applyWindowTheme(hwnd)
		return 1

	case wmDestroy:
		unthemeWindow(hwnd)
		return 0

	case wmCommand:
		id, code := wParam&0xFFFF, (wParam>>16)&0xFFFF
		switch {
		case id == idEdit && code == enChange:
			enableOK(hwnd)
			return 1
		case id == idOK:
			pass := readPassphrase(control(hwnd, idEdit))
			if len(pass) == 0 {
				return 1 // Enter on an empty field
			}
			dialog.result = pass
			procEndDialog.Call(hwnd, idOK)
			return 1
		case id == idExtra:
			procEndDialog.Call(hwnd, idExtra)
			return 1
		case id == idCancel: // also Esc and the close button
			procEndDialog.Call(hwnd, idCancel)
			return 1
		}
	}
	return 0
}

// enableOK enables the default button once the field holds something.
func enableOK(hwnd uintptr) {
	n, _, _ := procGetWindowTextLengthW.Call(control(hwnd, idEdit))
	procEnableWindow.Call(control(hwnd, idOK), boolArg(n > 0))
}

// phraseDialog is what the recovery phrase dialog shows.
var phraseDialog struct {
	sync.Mutex
	phrase string
}

// showRecoveryPhrase shows a new vault's recovery phrase until the user says
// it is saved. message says what it is for and where else it was written.
func showRecoveryPhrase(owner uintptr, message, phrase string) {
	phraseDialog.Lock()
	defer phraseDialog.Unlock()
	phraseDialog.phrase = phrase
	defer func() { phraseDialog.phrase = "" }()
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	runTemplate(owner, phraseTemplate(message), phraseDialogProc)
}

func phraseProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmInitDialog:
		edit := control(hwnd, idEdit)
		setText(edit, phraseLines(phraseDialog.phrase))
		themeWindow(hwnd)
		procSetForegroundWindow.Call(hwnd)
		procSetFocus.Call(control(hwnd, idOK))
		return 0

	case wmCtlColorStatic:
		if lParam == control(hwnd, idEdit) {
			return fieldCtlColor(wParam)
		}
		return themedCtlColor(msg, wParam, false)

	case wmCtlColorDlg, wmCtlColorBtn, wmCtlColorEdit:
		return themedCtlColor(msg, wParam, false)

	case wmThemeChanged:
		applyWindowTheme(hwnd)
		return 1

	case wmDestroy:
		setText(control(hwnd, idEdit), "")
		unthemeWindow(hwnd)
		return 0

	case wmCommand:
		switch wParam & 0xFFFF {
		case idExtra:
			if err := clipboard.CopySecret(phraseDialog.phrase); err != nil {
				messageBoxOwned(hwnd, "Could not copy the phrase: "+err.Error(), mbIconError)
			} else {
				setText(control(hwnd, idExtra), "Copied")
			}
			return 1
		case idOK:
			procEndDialog.Call(hwnd, idOK)
			return 1
		}
		// No Cancel: the dialog closes only once the user says the phrase is saved.
	}
	return 0
}

// readPassphrase takes the edit control's text as UTF-8 and empties the
// control. The control frees, but does not promise to wipe, its own copy.
func readPassphrase(edit uintptr) []byte {
	n, _, _ := procGetWindowTextLengthW.Call(edit)
	if n == 0 {
		return nil
	}
	buf := make([]uint16, n+1)
	defer clear(buf)
	got, _, _ := procGetWindowTextW.Call(edit, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	empty, _ := windows.UTF16PtrFromString("")
	procSetWindowTextW.Call(edit, uintptr(unsafe.Pointer(empty)))
	return utf16ToUTF8(buf[:got])
}

func control(hwnd uintptr, id int) uintptr {
	h, _, _ := procGetDlgItem.Call(hwnd, uintptr(id))
	return h
}

func boolArg(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// utf16ToUTF8 converts s, which may end in a NUL, without passing through a Go
// string, which could not be zeroed.
func utf16ToUTF8(s []uint16) []byte {
	for i, c := range s {
		if c == 0 {
			s = s[:i]
			break
		}
	}
	runes := utf16.Decode(s)
	defer clear(runes)
	out := make([]byte, 0, len(runes)*utf8.UTFMax)
	for _, r := range runes {
		out = utf8.AppendRune(out, r)
	}
	return out
}
