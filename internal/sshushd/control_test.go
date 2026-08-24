package sshushd

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ollykeran/sshush/internal/readypipe"
)

func TestStartServerDaemon_alreadyRunningPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	body := "[agent]\nsocket_path = \"\"\nvault = false\nkey_paths = [\"\"]\n\n[server]\nlisten_port = " + strconv.Itoa(port) + "\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	// Fix config so LoadConfig would succeed (socket_path and key_paths required)
	body = "[agent]\nsocket_path = \"" + dir + "/sock\"\nvault = false\nkey_paths = []\n\n[server]\nlisten_port = " + strconv.Itoa(port) + "\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	err = StartServerDaemon(configPath, port)
	if err == nil {
		t.Fatal("expected error when port already in use")
	}
	if err.Error() != "sshushd: server already running on port "+strconv.Itoa(port) {
		t.Errorf("expected \"sshushd: server already running on port %d\", got %q", port, err.Error())
	}
}

func TestStopDaemon_removesPidfile(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "test.pid")

	cmd := exec.Command("sh", "-c", "sleep 60")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleep process: %v", err)
	}
	pid := cmd.Process.Pid
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pidPath); err != nil {
		t.Fatal(err)
	}

	if err := StopDaemon(pidPath); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(pidPath); os.IsNotExist(err) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Errorf("pidfile should be removed after StopDaemon")
	}
}

func TestStartServerDaemon_pidFileRunning(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "sshush-server.pid")
	t.Setenv("XDG_RUNTIME_DIR", dir)

	cmd := exec.Command("sh", "-c", "sleep 60")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleep process: %v", err)
	}
	pid := cmd.Process.Pid
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(dir, "config.toml")
	body := "[agent]\nsocket_path = \"" + dir + "/sock\"\nvault = false\nkey_paths = []\n\n[server]\nlisten_port = " + strconv.Itoa(port) + "\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	err = StartServerDaemon(configPath, port)
	if err == nil {
		t.Fatal("expected error when pidfile has running process")
	}
	if !strings.Contains(err.Error(), "server already running on port "+strconv.Itoa(port)) {
		t.Errorf("expected 'server already running on port %d', got %q", port, err.Error())
	}
}

func TestStartDaemon_alreadyRunning(t *testing.T) {
	dir := unixSocketTempDir(t)
	socketPath := filepath.Join(dir, "daemon.sock")

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	err = StartDaemon("", socketPath)
	if err == nil {
		t.Fatal("expected error when daemon already running")
	}
	if !strings.Contains(err.Error(), "daemon already running") {
		t.Errorf("expected 'daemon already running' in error, got %q", err.Error())
	}
}

func TestStartDaemon_surfacesChildFailureMessage(t *testing.T) {
	binDir := t.TempDir()
	scriptPath := filepath.Join(binDir, "sshushd")
	script := "#!/bin/sh\neval \"printf '%s' 'stub failure message' >&${" + readypipe.EnvVar + "}\"\nexit 1\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	dir := unixSocketTempDir(t)
	socketPath := filepath.Join(dir, "daemon.sock")

	err := StartDaemon("", socketPath)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "stub failure message") {
		t.Errorf("expected error to contain the child's real message, got %q", err.Error())
	}
}

func TestStartDaemon_binaryNotFound(t *testing.T) {
	t.Setenv("PATH", "")
	dir := unixSocketTempDir(t)
	socketPath := filepath.Join(dir, "daemon.sock")

	err := StartDaemon("", socketPath)
	if err == nil {
		t.Fatal("expected error when binary not found")
	}
	if !strings.Contains(err.Error(), "binary not found") {
		t.Errorf("expected 'binary not found' in error, got %q", err.Error())
	}
}

func TestReloadDaemon_startFails(t *testing.T) {
	t.Setenv("PATH", "")
	dir := unixSocketTempDir(t)
	socketPath := filepath.Join(dir, "daemon.sock")
	pidPath := filepath.Join(dir, "nonexistent.pid")

	err := ReloadDaemon("", socketPath, pidPath)
	if err == nil {
		t.Fatal("expected error when reload fails")
	}
	if !strings.Contains(err.Error(), "reload failed") {
		t.Errorf("expected 'reload failed' in error, got %q", err.Error())
	}
}
