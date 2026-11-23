package tray

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/sshushd"
)

// StartAgent starts sshushd on socketPath for the config the CLI would use
// with no --config flag, and returns once it is serving or has failed. It is
// 'sshush start' without the terminal output; unlocking a vault is left to
// the caller.
func StartAgent(socketPath string) error {
	pidFile := runtime.PidFilePath()
	if sshushd.PidFileLive(pidFile) {
		return fmt.Errorf("sshushd is already running (pidfile %s) but is not answering; stop it and start it again", pidFile)
	}
	// A pidfile whose daemon is gone must not block the start.
	_ = os.Remove(pidFile)

	configPath := runtime.ConfigPathDefault()
	if _, err := os.Stat(configPath); err != nil {
		configPath = "" // sshushd reports the missing config itself
	} else if abs, err := filepath.Abs(configPath); err == nil {
		configPath = abs
	}
	return sshushd.StartDaemon(configPath, socketPath)
}

// StopAgent stops the sshushd named in the pidfile, as 'sshush stop' does.
func StopAgent() error {
	pidFile := runtime.PidFilePath()
	err := sshushd.StopDaemon(pidFile)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("sshushd is not running (no pidfile at %s)", pidFile)
	}
	return err
}
