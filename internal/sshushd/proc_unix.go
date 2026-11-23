//go:build !windows

package sshushd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// startDetached does nothing on Unix: the daemon detaches itself once it is
// running (see detachProcess).
func startDetached(*exec.Cmd) {}

// processAlive reports whether a process with this pid exists.
func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	return err == nil && process.Signal(syscall.Signal(0)) == nil
}

// daemonAlive reports whether pid is a running daemon. Unix has no cheap way to
// tell a daemon from whatever else may hold the pid, so any live process counts.
func daemonAlive(pid int) bool { return processAlive(pid) }

// requestStop asks the daemon to shut down, with SIGTERM.
func requestStop(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("sshushd: find process %d: %w", pid, err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("sshushd: send SIGTERM to process %d: %w", pid, err)
	}
	return nil
}

// forceStop does nothing on Unix, where a daemon that ignores SIGTERM is left
// for the user to deal with.
func forceStop(int) {}

// withStopRequest returns ctx unchanged on Unix: a stop arrives as SIGTERM,
// which the daemon's signal handling already turns into a cancelled context.
func withStopRequest(ctx context.Context) (context.Context, func()) {
	return ctx, func() {}
}
