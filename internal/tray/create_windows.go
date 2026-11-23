package tray

import (
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"unsafe"

	"github.com/ollykeran/sshush/internal/keys"
	"github.com/ollykeran/sshush/internal/utils"
	"golang.org/x/sys/windows"
)

// The create key dialog makes the key itself, so a problem, such as a name
// that is taken, is shown in it and can be fixed without starting again.

// createDialog is the open create key dialog's state.
var createDialog struct {
	sync.Mutex
	addLabel  string
	suggested string // the path last suggested, replaced when the kind changes
	created   string // the new key's path, once made
	add       bool
}

var createDialogProc = windows.NewCallback(createProc)

// promptCreateKey shows the create key dialog, owned by owner, and returns
// the new key's path and whether to add it to the agent. addLabel names that
// check box. It returns errCancelled when the user closes the dialog.
func promptCreateKey(owner uintptr, addLabel string) (path string, add bool, err error) {
	createDialog.Lock()
	defer createDialog.Unlock()
	createDialog.addLabel, createDialog.created, createDialog.add = addLabel, "", false
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	r, e := runTemplate(owner, createKeyTemplate(addLabel), createDialogProc)
	switch r {
	case idOK:
		return createDialog.created, createDialog.add, nil
	case idCancel:
		return "", false, errCancelled
	}
	return "", false, e
}

func sshDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh")
}

func createProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmInitDialog:
		combo := control(hwnd, idCreateType)
		for _, c := range keyChoices {
			p, _ := windows.UTF16PtrFromString(c.label)
			procSendMessageW.Call(combo, cbAddString, 0, uintptr(unsafe.Pointer(p)))
		}
		procSendMessageW.Call(combo, cbSetCurSel, 0, 0)
		setText(control(hwnd, idCreateComment), keys.DefaultComment())
		createDialog.suggested = utils.DisplayPath(suggestKeyPath(sshDir(), keyChoices[0].keyType))
		setText(control(hwnd, idCreatePath), createDialog.suggested)
		procSendMessageW.Call(control(hwnd, idCreateAdd), bmSetCheck, 1, 0)
		themeWindow(hwnd)
		procSetForegroundWindow.Call(hwnd)
		return 1 // the dialog manager focuses the first field

	case wmCtlColorStatic:
		return themedCtlColor(msg, wParam, lParam == control(hwnd, idError))

	case wmCtlColorDlg, wmCtlColorBtn, wmCtlColorEdit, wmCtlColorListBox:
		return themedCtlColor(msg, wParam, false)

	case wmThemeChanged:
		applyWindowTheme(hwnd)
		return 1

	case wmDestroy:
		unthemeWindow(hwnd)
		return 0

	case wmCommand:
		id, code := wParam&0xFFFF, (wParam>>16)&0xFFFF
		switch {
		case id == idCreateType && code == cbnSelChange:
			// Follow the kind with the suggested file name, unless the user
			// has chosen their own.
			path := control(hwnd, idCreatePath)
			if windowText(path) == createDialog.suggested {
				createDialog.suggested = utils.DisplayPath(suggestKeyPath(sshDir(), keyChoices[selectedChoice(hwnd)].keyType))
				setText(path, createDialog.suggested)
			}
			return 1
		case id == idCreateBrowse:
			p, err := pickSavePath(hwnd, utils.ExpandHomeDirectory(windowText(control(hwnd, idCreatePath))))
			if err == nil {
				setText(control(hwnd, idCreatePath), utils.DisplayPath(p))
			} else if !errors.Is(err, errCancelled) {
				setText(control(hwnd, idError), err.Error())
			}
			return 1
		case id == idOK:
			path := utils.ExpandHomeDirectory(windowText(control(hwnd, idCreatePath)))
			setText(control(hwnd, idError), "Creating…")
			err := CreateKey(selectedChoice(hwnd), windowText(control(hwnd, idCreateComment)), path)
			if err != nil {
				setText(control(hwnd, idError), describeCreateError(err))
				procRedrawWindow.Call(control(hwnd, idError), 0, 0, rdwInvalidate|rdwErase)
				procMessageBeep.Call(mbIconExclamation)
				return 1
			}
			add, _, _ := procSendMessageW.Call(control(hwnd, idCreateAdd), bmGetCheck, 0, 0)
			createDialog.created, createDialog.add = path, add == 1
			procEndDialog.Call(hwnd, idOK)
			return 1
		case id == idCancel:
			procEndDialog.Call(hwnd, idCancel)
			return 1
		}
	}
	return 0
}

func selectedChoice(hwnd uintptr) int {
	r, _, _ := procSendMessageW.Call(control(hwnd, idCreateType), cbGetCurSel, 0, 0)
	return int(int32(r))
}

// describeCreateError is the create dialog's error line for err.
func describeCreateError(err error) string {
	s := err.Error()
	if s == "" {
		return "Could not create the key."
	}
	return upperFirst(s) + "."
}

func upperFirst(s string) string {
	r := []rune(s)
	if len(r) > 0 && r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}
