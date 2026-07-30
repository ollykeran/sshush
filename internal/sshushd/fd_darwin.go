//go:build darwin

package sshushd

import "syscall"

func dupFd(fd, target int) {
	syscall.Dup2(fd, target)
}
