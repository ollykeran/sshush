package sshushd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"
)

// errNotDaemon reports a pid that is not a running sshushd: the daemon died
// without removing its pidfile, and the pid may since belong to something else.
var errNotDaemon = errors.New("not a running sshushd")

// FindBinary resolves the sshushd binary path.
func FindBinary() (string, error) {
	name := "sshushd"
	if goruntime.GOOS == "windows" {
		name += ".exe"
	}
	execPath, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(execPath)
		sshushdPath := filepath.Join(dir, name)
		if _, statErr := os.Stat(sshushdPath); statErr == nil {
			return sshushdPath, nil
		}
	}

	path, err := exec.LookPath(name)
	if err == nil {
		return path, nil
	}
	return "", fmt.Errorf("sshushd: binary not found in PATH or alongside current executable")
}

// StopDaemon asks the daemon named in pidFilePath to shut down — SIGTERM on
// Unix, its stop event on Windows — waits for it to exit, and removes the pidfile.
func StopDaemon(pidFilePath string) error {
	pid, err := readPidFile(pidFilePath)
	if err != nil {
		return err
	}
	if err := requestStop(pid); err != nil {
		if errors.Is(err, errNotDaemon) {
			// Nothing to stop, and a pidfile left behind would only block the next start.
			_ = os.Remove(pidFilePath)
		}
		return err
	}
	if !waitForExit(pid) {
		forceStop(pid)
		waitForExit(pid)
	}
	_ = os.Remove(pidFilePath)
	return nil
}

// PidFileLive reports whether pidFilePath names a daemon that is still running.
// A pidfile that cannot be read or parsed counts as live: better to refuse to
// start than to start a second daemon beside one we could not identify.
func PidFileLive(pidFilePath string) bool {
	pid, err := readPidFile(pidFilePath)
	if err != nil {
		return !errors.Is(err, os.ErrNotExist)
	}
	return daemonAlive(pid)
}

func readPidFile(pidFilePath string) (int, error) {
	data, err := os.ReadFile(pidFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("sshushd: no pidfile at %s: %w", pidFilePath, os.ErrNotExist)
		}
		return 0, fmt.Errorf("sshushd: read pidfile %s: %w", pidFilePath, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("sshushd: invalid pidfile %s: %w", pidFilePath, err)
	}
	return pid, nil
}

// waitForExit waits up to a second for the process to go and reports whether it did.
func waitForExit(pid int) bool {
	for i := 0; i < 50; i++ {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
