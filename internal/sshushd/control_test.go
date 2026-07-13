package sshushd

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
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
	if err.Error() != "already running" {
		t.Errorf("expected \"already running\", got %q", err.Error())
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
