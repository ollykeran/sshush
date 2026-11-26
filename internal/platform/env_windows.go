package platform

import (
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// PersistedEnv returns the value new processes will be given for the
// environment variable name: the user's own setting if there is one, otherwise
// the machine-wide one. That can differ from what this process has, because a
// process keeps the environment it started with — setting a variable with
// setx, or in System Properties, changes nothing in windows already open.
func PersistedEnv(name string) (string, bool) {
	for _, source := range []struct {
		root registry.Key
		path string
	}{
		{registry.CURRENT_USER, `Environment`},
		{registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`},
	} {
		key, err := registry.OpenKey(source.root, source.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		value, _, err := key.GetStringValue(name)
		_ = key.Close()
		if err == nil && value != "" {
			return value, true
		}
	}
	return "", false
}

// UserEnv returns the user's own persisted setting for the environment
// variable name, ignoring the machine-wide one.
func UserEnv(name string) (string, bool) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer key.Close()
	value, _, err := key.GetStringValue(name)
	return value, err == nil
}

// SetUserEnv persists the environment variable name for the user, as setx
// does, and tells running programs so: Explorer then gives the new value to
// the windows it opens. Windows already open keep the value they started with.
func SetUserEnv(name, value string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	err = key.SetStringValue(name, value)
	_ = key.Close()
	if err != nil {
		return err
	}
	broadcastEnvironmentChange()
	return nil
}

var procSendMessageTimeoutW = windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")

// broadcastEnvironmentChange sends WM_SETTINGCHANGE for "Environment" to every
// top-level window, waiting at most a few seconds for one that hangs.
func broadcastEnvironmentChange() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001A
		smtoAbortIfHung = 0x0002
		timeoutMillis   = 5000
	)
	area, _ := windows.UTF16PtrFromString("Environment")
	var result uintptr
	procSendMessageTimeoutW.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(area)), smtoAbortIfHung, timeoutMillis, uintptr(unsafe.Pointer(&result)))
}
