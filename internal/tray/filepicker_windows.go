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
