//go:build !windows

package sshushd

import (
	"os"
	"syscall"
)

// detachProcess makes the daemon leave the terminal that started it: a session of
// its own, and /dev/null for stdin, stdout and stderr.
func detachProcess() error {
	if _, err := syscall.Setsid(); err != nil {
		return err
	}
	devNull, err := os.OpenFile("/dev/null", os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()
	dupFd(int(devNull.Fd()), 0)
	dupFd(int(devNull.Fd()), 1)
	dupFd(int(devNull.Fd()), 2)
	return nil
}
