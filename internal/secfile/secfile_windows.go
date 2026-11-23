package secfile

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// privateDescriptor builds the security descriptor behind everything in this
// file: an access list naming the current user and SYSTEM, marked protected
// (the P) so that the folder's inherited entries, which are what would
// otherwise let other accounts in, are not merged into it. A directory's list
// is passed on to what is created inside it.
func privateDescriptor(isDir bool) (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("secfile: current user: %w", err)
	}
	flags := ""
	if isDir {
		flags = "OICI" // inherited by the files and folders inside
	}
	sddl := "D:P(A;" + flags + ";FA;;;" + user.User.Sid.String() + ")(A;" + flags + ";FA;;;SY)"
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return nil, fmt.Errorf("secfile: build access list: %w", err)
	}
	return sd, nil
}

// privateAttributes wraps privateDescriptor for the calls that create a file
// or directory, so the object comes into existence already carrying the list.
// Creating it first and restricting it afterwards leaves a moment in which it
// has whatever its folder hands down, and a handle another account opens in
// that moment keeps its access however the list changes later.
func privateAttributes(isDir bool) (*windows.SecurityAttributes, error) {
	sd, err := privateDescriptor(isDir)
	if err != nil {
		return nil, err
	}
	sa := &windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(*sa))
	return sa, nil
}

// restrict replaces path's access list with the private one.
func restrict(path string, isDir bool) error {
	sd, err := privateDescriptor(isDir)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("secfile: build access list: %w", err)
	}
	err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
	if err != nil {
		return fmt.Errorf("secfile: restrict %s: %w", path, err)
	}
	return nil
}

// The sharing os.OpenFile asks for, so a file opened here behaves like any
// other Go file to whoever else has it open.
const shareMode = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE

// createPrivate creates path, which must not exist, with the private access
// list in place from the first instant.
func createPrivate(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	sa, err := privateAttributes(false)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, shareMode, sa,
		windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		// A PathError around the raw code, as os.OpenFile returns, so that
		// errors.Is(err, os.ErrExist) holds when the file is already there.
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// openPrivate opens path for WriteFile: private and empty, whether or not it
// was there before.
//
// Windows only honours security attributes when the call creates the file, so
// an existing one takes a second route: opened as it is, restricted, and only
// then emptied. That order means nobody shut out by the new list can open the
// file once the new content starts to go in. What cannot be helped is a handle
// another account already had open on the old file: access is checked when a
// handle is opened, not when it is used, so that handle goes on reading. Such
// a reader could already read what was there; a caller that cannot accept it
// reading the new content has to write to a fresh name instead (which is what
// the vault does: a new file beside itself, then a rename).
func openPrivate(path string) (*os.File, error) {
	// Each step can lose a race with someone creating or deleting the file, in
	// which case the other step is the right one; a few rounds is plenty short
	// of something doing it on purpose.
	var err error
	for range 3 {
		var f *os.File
		if f, err = createPrivate(path); err == nil || !errors.Is(err, os.ErrExist) {
			return f, err
		}
		if f, err = restrictExisting(path); err == nil || !errors.Is(err, os.ErrNotExist) {
			return f, err
		}
	}
	return nil, err
}

// restrictExisting opens the file already at path, replaces its access list
// and empties it, in that order. The list is set through the handle rather
// than by name so that it lands on the file that was opened, whatever happens
// to the name meanwhile; that takes WRITE_DAC on top of write access.
func restrictExisting(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	sd, err := privateDescriptor(false)
	if err != nil {
		return nil, err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return nil, fmt.Errorf("secfile: build access list: %w", err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE|windows.WRITE_DAC, shareMode, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	f := os.NewFile(uintptr(h), path)
	err = windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("secfile: restrict %s: %w", path, err)
	}
	if err := f.Truncate(0); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// mkdirPrivate creates the single directory dir with the private access list,
// which it passes on to what is created inside it.
func mkdirPrivate(dir string) error {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return &os.PathError{Op: "mkdir", Path: dir, Err: err}
	}
	sa, err := privateAttributes(true)
	if err != nil {
		return err
	}
	if err := windows.CreateDirectory(p, sa); err != nil {
		return &os.PathError{Op: "mkdir", Path: dir, Err: err}
	}
	return nil
}

// isPrivate reports whether every entry in path's access list names the
// current user, SYSTEM or Administrators. The last two can read any file on
// the machine whatever its list says, so finding them there gives nothing away.
func isPrivate(path string, _ os.FileInfo) (bool, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false, fmt.Errorf("secfile: read access list of %s: %w", path, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return false, fmt.Errorf("secfile: read access list of %s: %w", path, err)
	}
	if dacl == nil {
		// No list at all means everyone has full access.
		return false, nil
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false, fmt.Errorf("secfile: current user: %w", err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return false, err
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return false, err
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return false, err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			// A deny entry only ever takes access away.
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.Equals(user.User.Sid) && !sid.Equals(system) && !sid.Equals(admins) {
			return false, nil
		}
	}
	return true, nil
}
