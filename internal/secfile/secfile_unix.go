//go:build !windows

package secfile

import "os"

func restrict(path string, isDir bool) error {
	if isDir {
		return os.Chmod(path, 0o700)
	}
	return os.Chmod(path, 0o600)
}

func isPrivate(_ string, info os.FileInfo) (bool, error) {
	return info.Mode().Perm()&0o077 == 0, nil
}
