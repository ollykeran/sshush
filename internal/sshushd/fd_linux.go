//go:build linux

package sshushd

import "golang.org/x/sys/unix"

func dupFd(fd, target int) {
	unix.Dup2(fd, target)
}
