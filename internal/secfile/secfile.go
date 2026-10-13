// Package secfile writes files and directories that only the current user can
// read: vaults, private keys, recovery phrases. On Unix that is a file mode
// (0600, 0700). Windows has no modes worth the name — os.Chmod only toggles the
// read-only bit, and a new file inherits its folder's access list — so there
// the same promise is kept with an explicit access control list.
package secfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// WriteFile writes data to path, creating or replacing it, so that only the
// current user can read it. The file is private before the data goes in: a new
// one is created that way, and one that is already there is closed to others
// before it is emptied and rewritten.
func WriteFile(path string, data []byte) error {
	f, err := openPrivate(path)
	if err != nil {
		return err
	}
	return writeAndClose(f, data)
}

// WriteNewFile is WriteFile for a file that must not be there yet: if path
// already exists it is left untouched and the error satisfies
// errors.Is(err, os.ErrExist). Two processes racing to write the same new file
// (two servers generating a host key, say) therefore cannot both win.
func WriteNewFile(path string, data []byte) error {
	f, err := createPrivate(path)
	if err != nil {
		return err
	}
	if err := writeAndClose(f, data); err != nil {
		// The file is this call's own, and half a secret is no use to anyone;
		// left behind it would also make the next attempt fail as "exists".
		_ = os.Remove(path)
		return err
	}
	return nil
}

func writeAndClose(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	// Synced because what is written here tends to be the only copy of a secret.
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// MkdirAll creates dir and any missing parents. The directory itself is
// private to the current user if this call creates it; one that already
// exists is left as it is, the way os.MkdirAll leaves an existing directory's
// mode alone.
func MkdirAll(dir string) error {
	dir = filepath.Clean(dir)
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", dir)
		}
		return nil
	}
	// The parents the ordinary way, then the leaf on its own so that it can be
	// created private rather than created and then closed off.
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return err
	}
	if err := mkdirPrivate(dir); err != nil {
		// Someone else made it between the Stat above and here: an existing
		// directory, to be left alone like any other.
		if info, statErr := os.Stat(dir); errors.Is(err, os.ErrExist) && statErr == nil && info.IsDir() {
			return nil
		}
		return err
	}
	return nil
}

// Restrict makes the existing file or directory at path accessible to the
// current user only.
func Restrict(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return restrict(path, info.IsDir())
}

// IsPrivate reports whether only the current user (and the accounts that can
// read anything regardless: root, or SYSTEM and Administrators) can read path.
func IsPrivate(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return isPrivate(path, info)
}
