package sshushd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
)

// stopEventNames returns the names of the event a daemon with this pid listens
// on for a stop request, in the order to try them. The global name lets a stop
// reach a daemon started from another logon session (the agent's pipe is
// machine-wide, so that can happen); the local one is the fallback for when
// the global namespace is not available.
func stopEventNames(pid int) []string {
	suffix := `sshush-stop-` + strconv.Itoa(pid)
	return []string{`Global\` + suffix, `Local\` + suffix}
}

// openStopEvent opens the stop event of the daemon with this pid. Only a live
// sshushd holds one, which makes it the proof that pid is ours to stop.
func openStopEvent(pid int) (windows.Handle, error) {
	var lastErr error
	for _, name := range stopEventNames(pid) {
		ptr, err := windows.UTF16PtrFromString(name)
		if err != nil {
			return 0, err
		}
		h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE|windows.SYNCHRONIZE, false, ptr)
		if err == nil {
			return h, nil
		}
		lastErr = err
	}
	return 0, lastErr
}

// startDetached makes cmd start with no console and in a process group of its
// own, so closing the terminal that ran `sshush start`, or a Ctrl+C typed
// there, does not take the daemon with it. Windows has no setsid: a process
// cannot leave its console later, so it has to be created outside one.
func startDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}

// processAlive reports whether a process with this pid exists and has not exited.
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		// Access denied means there is a process, just not one of ours.
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h)
	state, err := windows.WaitForSingleObject(h, 0)
	return err == nil && state == uint32(windows.WAIT_TIMEOUT)
}

// daemonAlive reports whether pid is a running sshushd, as opposed to an
// unrelated process that was handed a dead daemon's pid.
func daemonAlive(pid int) bool {
	h, err := openStopEvent(pid)
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

// requestStop asks the daemon to shut down by setting its stop event. Windows
// has no SIGTERM to send to another process.
func requestStop(pid int) error {
	h, err := openStopEvent(pid)
	if err != nil {
		return fmt.Errorf("sshushd: process %d: %w", pid, errNotDaemon)
	}
	defer windows.CloseHandle(h)
	if err := windows.SetEvent(h); err != nil {
		return fmt.Errorf("sshushd: signal process %d to stop: %w", pid, err)
	}
	return nil
}

// forceStop terminates a daemon that did not act on requestStop. Call it only
// after requestStop succeeded, which is what establishes that pid is a daemon.
func forceStop(pid int) {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_ = windows.TerminateProcess(h, 1)
}

// withStopRequest returns a context that is also cancelled when another
// process asks this daemon to stop (see requestStop). The returned function
// releases the event; call it when the daemon is done.
func withStopRequest(ctx context.Context) (context.Context, func()) {
	var event windows.Handle
	for _, name := range stopEventNames(os.Getpid()) {
		ptr, err := windows.UTF16PtrFromString(name)
		if err != nil {
			continue
		}
		// Manual reset, initially unset.
		if h, err := windows.CreateEvent(nil, 1, 0, ptr); err == nil {
			event = h
			break
		}
	}
	if event == 0 {
		// Without the event `sshush stop` cannot reach this daemon, but an
		// agent that serves keys is still better than one that refuses to start.
		return ctx, func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = windows.WaitForSingleObject(event, windows.INFINITE)
		cancel()
	}()
	return ctx, func() {
		// Setting the event is what releases the goroutine above.
		_ = windows.SetEvent(event)
		<-done
		_ = windows.CloseHandle(event)
	}
}
