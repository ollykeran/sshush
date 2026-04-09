package sshushd

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ollykeran/sshush/internal/runtime"
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

	env := os.Environ()
	if configPath != "" {
		env = append(env, "SSHUSH_CONFIG="+configPath)
	}

	child := exec.Command(sshushdPath)
	child.Env = env
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

// StartServerDaemon starts the SSH server daemon (sshushd --server) with SSHUSH_CONFIG and waits for TCP listen.
func StartServerDaemon(configPath string, port int) error {
	pidFilePath := runtime.ServerPidFilePath()
	data, err := os.ReadFile(pidFilePath)
	if err == nil {
		pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		if pid > 0 {
			if process, findErr := os.FindProcess(pid); findErr == nil && process.Signal(syscall.Signal(0)) == nil {
				return fmt.Errorf("already running")
			}
		}
	}
	addr := "127.0.0.1:" + strconv.Itoa(port)
	if conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond); err == nil {
		conn.Close()
		return fmt.Errorf("already running")
	}

	sshushdPath, err := FindBinary()
	if err != nil {
		return err
	}
	env := os.Environ()
	if configPath != "" {
		env = append(env, "SSHUSH_CONFIG="+configPath)
	}
	child := exec.Command(sshushdPath, "--server")
	child.Env = env
	child.Stdin = nil
	child.Stdout = nil
	child.Stderr = nil
	if err := child.Start(); err != nil {
		return fmt.Errorf("start failed: %w", err)
	}
	for i := 0; i < 50; i++ {
		if conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond); err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("started but port %d not ready", port)
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
