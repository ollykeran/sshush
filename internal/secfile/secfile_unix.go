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

// openPrivate opens path for WriteFile. The mode keeps a file created here
// closed to others from the start; the chmod is for one that was already there
// with a wider mode, which O_CREATE leaves as it found it.
func openPrivate(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	if err := restrict(path, false); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// createPrivate creates path, which must not exist, readable by its owner only.
func createPrivate(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

// mkdirPrivate creates the single directory dir. The chmod is belt and braces:
// a umask can only take bits away from 0700, never add any.
func mkdirPrivate(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	return restrict(dir, true)
}
