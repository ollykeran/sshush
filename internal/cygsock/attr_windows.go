package cygsock

import "golang.org/x/sys/windows"

// markSocket sets the system attribute, which is how Cygwin tells a socket
// file from an ordinary one.
func markSocket(path string) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.SetFileAttributes(p, windows.FILE_ATTRIBUTE_SYSTEM)
}

// clearAttributes makes path an ordinary file again, if it exists.
func clearAttributes(path string) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	_ = windows.SetFileAttributes(p, windows.FILE_ATTRIBUTE_NORMAL)
}
