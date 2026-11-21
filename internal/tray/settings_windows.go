package tray

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// The tray keeps its own settings in the user's registry, beside the theme:
// where the main window was, and whether Windows starts the tray at sign-in.

const (
	runKey        = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue      = "sshush"
	startupSwitch = "--background"
)

// saveSetting stores b under name, quietly: a setting that cannot be saved
// is only forgotten.
func saveSetting(name string, b []byte) {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, settingsKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.SetBinaryValue(name, b)
}

// loadSetting returns what saveSetting stored under name, or nil.
func loadSetting(name string) []byte {
	k, err := registry.OpenKey(registry.CURRENT_USER, settingsKey, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	b, _, err := k.GetBinaryValue(name)
	if err != nil {
		return nil
	}
	return b
}

// startupCommand is how Windows starts this tray at sign-in: in the
// notification area, without opening the window.
func startupCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if abs, err := filepath.EvalSymlinks(exe); err == nil {
		exe = abs
	}
	return `"` + exe + `" ` + startupSwitch, nil
}

// startupEnabled reports whether Windows starts sshush at sign-in.
func startupEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValue)
	return err == nil
}

// setStartup makes Windows start this tray at sign-in, or stop doing so.
func setStartup(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue(runValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	cmd, err := startupCommand()
	if err != nil {
		return err
	}
	return k.SetStringValue(runValue, cmd)
}

// refreshStartup points an existing sign-in entry at this program, so one
// left by an install that has since moved still starts the tray.
func refreshStartup() {
	if !startupEnabled() {
		return
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	cmd, err := startupCommand()
	if err != nil {
		return
	}
	if cur, _, err := k.GetStringValue(runValue); err == nil && cur != cmd {
		_ = k.SetStringValue(runValue, cmd)
	}
}
