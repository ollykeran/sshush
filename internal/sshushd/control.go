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

	"github.com/ollykeran/sshush/internal/readypipe"
	"github.com/ollykeran/sshush/internal/runtime"
)

// readyTimeout is the dead-man's-switch: how long the parent waits for the
// child to signal readiness (or failure) over the readiness pipe before
// giving up. It is a safety net for a truly hung child, not the primary
// mechanism — the child signals as soon as it actually knows its state.
const readyTimeout = 5 * time.Second

// StartDaemon starts sshushd with SSHUSH_CONFIG and waits for socket readiness.
func StartDaemon(configPath, socketPath string) error {
	if CheckAlreadyRunning(socketPath) {
		return fmt.Errorf("sshushd: daemon already running on %s", socketPath)
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

	rp, err := readypipe.New()
	if err != nil {
		return fmt.Errorf("sshushd: readiness pipe: %w", err)
	}
	defer rp.Close()
	rp.Attach(child)

	if err := child.Start(); err != nil {
		return fmt.Errorf("sshushd: start failed: %w", err)
	}
	rp.CloseWrite()

	if err := rp.Wait(readyTimeout); err != nil {
		return fmt.Errorf("sshushd: %w", err)
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
				return fmt.Errorf("sshushd: server already running on port %d", port)
			}
		}
	}
	addr := "127.0.0.1:" + strconv.Itoa(port)
	if conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond); err == nil {
		conn.Close()
		return fmt.Errorf("sshushd: server already running on port %d", port)
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

	rp, err := readypipe.New()
	if err != nil {
		return fmt.Errorf("sshushd: readiness pipe: %w", err)
	}
	defer rp.Close()
	rp.Attach(child)

	if err := child.Start(); err != nil {
		return fmt.Errorf("sshushd: start server failed: %w", err)
	}
	rp.CloseWrite()

	if err := rp.Wait(readyTimeout); err != nil {
		return fmt.Errorf("sshushd: %w", err)
	}
	return nil
}

// ReloadDaemon stops any existing daemon and starts a new one.
func ReloadDaemon(configPath, socketPath, pidFilePath string) error {
	_ = StopDaemon(pidFilePath)
	time.Sleep(100 * time.Millisecond)
	if err := StartDaemon(configPath, socketPath); err != nil {
		return fmt.Errorf("sshushd: reload failed: %w", err)
	}
	return nil
}
