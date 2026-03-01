package sshushd

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

// StartDaemon starts sshushd with SSHUSH_CONFIG and waits for socket readiness.
func StartDaemon(configPath, socketPath string) error {
	if CheckAlreadyRunning(socketPath) {
		return fmt.Errorf("already running")
	}

	sshushdPath, err := FindBinary()
	if err != nil {
		return err
	}

	child := exec.Command(sshushdPath)
	if configPath != "" {
		child.Env = append(os.Environ(), "SSHUSH_CONFIG="+configPath)
	}
	child.Stdin = nil
	child.Stdout = nil
	child.Stderr = nil
	if err := child.Start(); err != nil {
		return fmt.Errorf("start failed: %w", err)
	}
	if !WaitForSocket(socketPath, 50, 10*time.Millisecond) {
		return fmt.Errorf("started but socket not ready")
	}
	return nil
}

// ReloadDaemon stops any existing daemon and starts a new one.
func ReloadDaemon(configPath, socketPath, pidFilePath string) error {
	_ = StopDaemon(pidFilePath)
	time.Sleep(100 * time.Millisecond)
	if err := StartDaemon(configPath, socketPath); err != nil {
		return fmt.Errorf("reload failed: %w", err)
	}
	return nil
}
