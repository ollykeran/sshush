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
	return "", fmt.Errorf("sshushd binary not found")
}

// StopDaemon sends SIGTERM to the process in pidFilePath and waits for it to exit.
func StopDaemon(pidFilePath string) error {
	data, err := os.ReadFile(pidFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no pidfile at %s: %w", pidFilePath, os.ErrNotExist)
		}
		return err
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("invalid pidfile: %w", err)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("send SIGTERM: %w", err)
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
