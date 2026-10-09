// Package secfile writes files and directories that only the current user can
// read: vaults, private keys, recovery phrases. On Unix that is a file mode
// (0600, 0700). Windows has no modes worth the name — os.Chmod only toggles the
// read-only bit, and a new file inherits its folder's access list — so there
// the same promise is kept with an explicit access control list.
package secfile

import (
	"fmt"
	"os"
)

// WriteFile writes data to path, creating or replacing it, so that only the
// current user can read it. The file is made private before the data goes in.
func WriteFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := Restrict(path); err != nil {
		_ = f.Close()
		return err
	}
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

// MkdirAll creates dir and any missing parents. A directory it creates is
// private to the current user; one that already exists is left as it is, the
// way os.MkdirAll leaves an existing directory's mode alone.
func MkdirAll(dir string) error {
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", dir)
		}
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return Restrict(dir)
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
