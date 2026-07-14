package sshushd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// FindBinary resolves the sshushd binary path.
func FindBinary() (string, error) {
	execPath, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(execPath)
		sshushdPath := filepath.Join(dir, "sshushd")
		if _, statErr := os.Stat(sshushdPath); statErr == nil {
			return sshushdPath, nil
		}
	}

	path, err := exec.LookPath("sshushd")
	if err == nil {
		return path, nil
	}
	return "", fmt.Errorf("sshushd: binary not found in PATH or alongside current executable")
}

// StopDaemon sends SIGTERM to the process in pidFilePath and waits for it to exit.
func StopDaemon(pidFilePath string) error {
	data, err := os.ReadFile(pidFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("sshushd: no pidfile at %s: %w", pidFilePath, os.ErrNotExist)
		}
		return fmt.Errorf("sshushd: read pidfile %s: %w", pidFilePath, err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("sshushd: invalid pidfile %s: %w", pidFilePath, err)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("sshushd: find process %d: %w", pid, err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("sshushd: send SIGTERM to process %d: %w", pid, err)
	}
	for i := 0; i < 50; i++ {
		if process.Signal(syscall.Signal(0)) != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = os.Remove(pidFilePath)
	return nil
}
