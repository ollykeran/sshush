package tray

import (
	"errors"
	"fmt"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows' own credential prompt, the one Windows itself uses to ask for a
// password, reached through credui.dll by syscall so no cgo is needed.
var (
	credui                           = windows.NewLazySystemDLL("credui.dll")
	procCredUIPromptForWindowsCreds  = credui.NewProc("CredUIPromptForWindowsCredentialsW")
	procCredPackAuthenticationBuffer = credui.NewProc("CredPackAuthenticationBufferW")
	procCredUnPackAuthenticationBuf  = credui.NewProc("CredUnPackAuthenticationBufferW")
)

const (
	creduiwinGeneric            = 0x1 // plain user name and password, no Windows logon packages
	credPackGenericCredentials  = 0x4
	errorCancelled              = 1223
	errorInsufficientBuffer     = 122
	errorLogonFailure           = 1326 // makes the dialog say the password was wrong
	promptUserName              = "sshush vault"
	maxUserNameChars            = 256
	initialPasswordBufferChars  = 1024
	initialDomainNameBufferSize = 256
)

// creduiInfo is CREDUI_INFOW.
type creduiInfo struct {
	cbSize         uint32
	hwndParent     windows.HWND
	pszMessageText *uint16
	pszCaptionText *uint16
	hbmBanner      windows.Handle
}

// errCancelled reports that the user dismissed the prompt.
var errCancelled = errors.New("tray: prompt cancelled")

// promptPassphrase shows the credential prompt with the user name fixed to
// promptUserName and returns what was typed as UTF-8. wrongLast makes the
// dialog say the previous attempt was wrong. The caller owns and zeroes the
// returned bytes.
func promptPassphrase(message string, wrongLast bool) ([]byte, error) {
	caption, _ := windows.UTF16PtrFromString("sshush")
	msg, _ := windows.UTF16PtrFromString(message)
	info := creduiInfo{pszMessageText: msg, pszCaptionText: caption}
	info.cbSize = uint32(unsafe.Sizeof(info))

	in, err := packUserName(promptUserName)
	if err != nil {
		return nil, err
	}
	var authError uintptr
	if wrongLast {
		authError = errorLogonFailure
	}
	var (
		authPackage uint32
		out         *byte // allocated by credui, freed with CoTaskMemFree
		outSize     uint32
	)
	r, _, _ := procCredUIPromptForWindowsCreds.Call(
		uintptr(unsafe.Pointer(&info)),
		authError,
		uintptr(unsafe.Pointer(&authPackage)),
		uintptr(unsafe.Pointer(&in[0])),
		uintptr(len(in)),
		uintptr(unsafe.Pointer(&out)),
		uintptr(unsafe.Pointer(&outSize)),
		0, // no "remember me" checkbox
		creduiwinGeneric,
	)
	switch r {
	case 0:
	case errorCancelled:
		return nil, errCancelled
	default:
		return nil, fmt.Errorf("tray: credential prompt: %w", windows.Errno(r))
	}
	defer func() {
		// The buffer holds the passphrase; wipe it before handing it back.
		clear(unsafe.Slice(out, outSize))
		windows.CoTaskMemFree(unsafe.Pointer(out))
	}()
	return unpackPassword(out, outSize)
}

// packUserName builds the input buffer that prefills the user name field.
func packUserName(name string) ([]byte, error) {
	user, _ := windows.UTF16PtrFromString(name)
	empty, _ := windows.UTF16PtrFromString("")
	var size uint32
	// The first call only reports the size it needs, and fails doing so.
	procCredPackAuthenticationBuffer.Call(credPackGenericCredentials,
		uintptr(unsafe.Pointer(user)), uintptr(unsafe.Pointer(empty)), 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return nil, errors.New("tray: pack credential buffer: no size reported")
	}
	buf := make([]byte, size)
	r, _, e := procCredPackAuthenticationBuffer.Call(credPackGenericCredentials,
		uintptr(unsafe.Pointer(user)), uintptr(unsafe.Pointer(empty)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return nil, fmt.Errorf("tray: pack credential buffer: %w", e)
	}
	return buf[:size], nil
}

// unpackPassword extracts the password from the prompt's output buffer as
// UTF-8, zeroing every intermediate copy.
func unpackPassword(buf *byte, size uint32) ([]byte, error) {
	userLen, domainLen, passLen := uint32(maxUserNameChars), uint32(initialDomainNameBufferSize), uint32(initialPasswordBufferChars)
	for attempt := 0; attempt < 2; attempt++ {
		user := make([]uint16, userLen)
		domain := make([]uint16, domainLen)
		pass := make([]uint16, passLen)
		r, _, e := procCredUnPackAuthenticationBuf.Call(credPackGenericCredentials, uintptr(unsafe.Pointer(buf)), uintptr(size),
			uintptr(unsafe.Pointer(&user[0])), uintptr(unsafe.Pointer(&userLen)),
			uintptr(unsafe.Pointer(&domain[0])), uintptr(unsafe.Pointer(&domainLen)),
			uintptr(unsafe.Pointer(&pass[0])), uintptr(unsafe.Pointer(&passLen)))
		if r != 0 {
			defer clear(pass)
			return utf16ToUTF8(pass[:passLen]), nil
		}
		clear(pass)
		if !errors.Is(e, windows.Errno(errorInsufficientBuffer)) {
			return nil, fmt.Errorf("tray: unpack credential buffer: %w", e)
		}
		// The lengths now hold what the call needs; go round once more.
	}
	return nil, errors.New("tray: unpack credential buffer: buffer still too small")
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
