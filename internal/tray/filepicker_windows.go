package tray

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Explorer's Open dialog, through comdlg32 by syscall. GetOpenFileNameW is
// the classic entry point; with OFN_EXPLORER Windows shows its current
// Explorer-style dialog.
var (
	comdlg32                 = windows.NewLazySystemDLL("comdlg32.dll")
	procGetOpenFileNameW     = comdlg32.NewProc("GetOpenFileNameW")
	procCommDlgExtendedError = comdlg32.NewProc("CommDlgExtendedError")
)

const (
	pickerBufferChars = 32 * 1024 // room for many selected names
	pickerFilter      = "SSH private keys (id_*, *.pem, *.key)\x00id_*;*.pem;*.key\x00All files (*.*)\x00*.*\x00\x00"
	ofnFlags          = 0x00080000 | // OFN_EXPLORER
		0x00000200 | // OFN_ALLOWMULTISELECT
		0x00001000 | // OFN_FILEMUSTEXIST
		0x00000800 | // OFN_PATHMUSTEXIST
		0x00000008 | // OFN_NOCHANGEDIR
		0x00000004 | // OFN_HIDEREADONLY
		0x02000000 // OFN_DONTADDTORECENT: key files do not belong in Recent
)

// openFileName is OPENFILENAMEW.
type openFileName struct {
	structSize      uint32
	owner           windows.HWND
	instance        windows.Handle
	filter          *uint16
	customFilter    *uint16
	maxCustomFilter uint32
	filterIndex     uint32
	file            *uint16
	maxFile         uint32
	fileTitle       *uint16
	maxFileTitle    uint32
	initialDir      *uint16
	title           *uint16
	flags           uint32
	fileOffset      uint16
	fileExtension   uint16
	defExt          *uint16
	custData        uintptr
	hook            uintptr
	templateName    *uint16
	reserved        uintptr
	reservedDword   uint32
	flagsEx         uint32
}

// pickKeyFiles shows Explorer's Open dialog for one or more private key
// files, starting in ~/.ssh, and returns their full paths. It returns
// errCancelled when the user closes the dialog.
func pickKeyFiles() ([]string, error) {
	// The dialog hosts shell components, which want a single-threaded COM
	// apartment on the thread that shows it.
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err == nil {
		defer windows.CoUninitialize()
	}

	buf := make([]uint16, pickerBufferChars)
	filter := utf16.Encode([]rune(pickerFilter)) // UTF16FromString would stop at the first NUL
	title, _ := windows.UTF16PtrFromString("Add keys to sshush")
	ofn := openFileName{
		filter:      &filter[0],
		filterIndex: 1,
		file:        &buf[0],
		maxFile:     uint32(len(buf)),
		title:       title,
		flags:       ofnFlags,
	}
	ofn.structSize = uint32(unsafe.Sizeof(ofn))
	if home, err := os.UserHomeDir(); err == nil {
		if dir := filepath.Join(home, ".ssh"); isDir(dir) {
			ofn.initialDir, _ = windows.UTF16PtrFromString(dir)
		}
	}

	r, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		code, _, _ := procCommDlgExtendedError.Call()
		if code == 0 {
			return nil, errCancelled
		}
		return nil, fmt.Errorf("tray: file dialog failed (code %#x)", code)
	}
	return splitPickerResult(buf), nil
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

var procGetSaveFileNameW = comdlg32.NewProc("GetSaveFileNameW")

const saveFlags = 0x00080000 | // OFN_EXPLORER
	0x00000800 | // OFN_PATHMUSTEXIST
	0x00000002 | // OFN_OVERWRITEPROMPT
	0x00000008 | // OFN_NOCHANGEDIR
	0x00000004 | // OFN_HIDEREADONLY
	0x02000000 // OFN_DONTADDTORECENT

// pickSavePath shows Explorer's Save dialog for a new private key file,
// owned by owner and starting at suggested, and returns the chosen path. It
// returns errCancelled when the user closes the dialog.
func pickSavePath(owner uintptr, suggested string) (string, error) {
	return pickSaveFile(owner, suggested, "Save the new key as", "All files (*.*)\x00*.*\x00\x00", "")
}

// vaultFilter is the Open and Save dialogs' filter for vault files.
const vaultFilter = "sshush vaults (*.json)\x00*.json\x00All files (*.*)\x00*.*\x00\x00"

// pickVaultBackup shows the Save dialog for a copy of the vault, starting at
// suggested.
func pickVaultBackup(owner uintptr, suggested string) (string, error) {
	return pickSaveFile(owner, suggested, "Back up the vault to", vaultFilter, "json")
}

// pickSaveFile shows Explorer's Save dialog, owned by owner, with title and
// filter (pairs of NUL-terminated strings, ended by another NUL), starting at
// suggested, and returns the chosen path. defExt, without its dot, is added to
// a name typed without one. It returns errCancelled when the user closes the
// dialog.
func pickSaveFile(owner uintptr, suggested, title, filter, defExt string) (string, error) {
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err == nil {
		defer windows.CoUninitialize()
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	copy(buf, utf16.Encode([]rune(filepath.Base(suggested))))
	filterW := utf16.Encode([]rune(filter))
	titleW, _ := windows.UTF16PtrFromString(title)
	ofn := openFileName{
		owner:       windows.HWND(owner),
		filter:      &filterW[0],
		filterIndex: 1,
		file:        &buf[0],
		maxFile:     uint32(len(buf)),
		title:       titleW,
		flags:       saveFlags,
	}
	if defExt != "" {
		ofn.defExt, _ = windows.UTF16PtrFromString(defExt)
	}
	ofn.structSize = uint32(unsafe.Sizeof(ofn))
	if dir := filepath.Dir(suggested); isDir(dir) {
		ofn.initialDir, _ = windows.UTF16PtrFromString(dir)
	}
	if r, _, _ := procGetSaveFileNameW.Call(uintptr(unsafe.Pointer(&ofn))); r == 0 {
		if code, _, _ := procCommDlgExtendedError.Call(); code != 0 {
			return "", fmt.Errorf("tray: file dialog failed (code %#x)", code)
		}
		return "", errCancelled
	}
	return windows.UTF16ToString(buf), nil
}

// pickVaultFile shows Explorer's Open dialog for one vault file, owned by
// owner, and returns its path. It returns errCancelled when the user closes
// the dialog.
func pickVaultFile(owner uintptr) (string, error) {
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err == nil {
		defer windows.CoUninitialize()
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	filter := utf16.Encode([]rune(vaultFilter))
	title, _ := windows.UTF16PtrFromString("Import a vault")
	ofn := openFileName{
		owner:       windows.HWND(owner),
		filter:      &filter[0],
		filterIndex: 1,
		file:        &buf[0],
		maxFile:     uint32(len(buf)),
		title:       title,
		flags:       ofnFlags &^ 0x00000200, // one file: not OFN_ALLOWMULTISELECT
	}
	ofn.structSize = uint32(unsafe.Sizeof(ofn))
	r, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		if code, _, _ := procCommDlgExtendedError.Call(); code != 0 {
			return "", fmt.Errorf("tray: file dialog failed (code %#x)", code)
		}
		return "", errCancelled
	}
	return windows.UTF16ToString(buf), nil
}
