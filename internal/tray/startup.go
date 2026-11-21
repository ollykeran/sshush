package tray

import "errors"

// startupTaskState is Windows.ApplicationModel.StartupTaskState: whether the
// package's StartupTask (see packaging/msix/AppxManifest.xml.in) starts the
// tray at sign-in, and who decided.
type startupTaskState int32

const (
	taskDisabled         startupTaskState = 0
	taskDisabledByUser   startupTaskState = 1
	taskEnabled          startupTaskState = 2
	taskDisabledByPolicy startupTaskState = 3
	taskEnabledByPolicy  startupTaskState = 4
)

// startupTaskID is the TaskId the manifest gives the StartupTask.
const startupTaskID = "SshushTray"

// on reports whether the task starts the tray at sign-in.
func (s startupTaskState) on() bool {
	return s == taskEnabled || s == taskEnabledByPolicy
}

// errStartupInSettings says the user turned the task off in Settings, and
// only Settings can turn it back on: Windows will not let the app itself
// overrule them.
var errStartupInSettings = errors.New("it was turned off in Settings, so turn it on there, under Apps > Startup")

// errStartupByPolicy says the organisation's policy decides, not the user.
var errStartupByPolicy = errors.New("your organisation's policy decides whether it starts at sign-in")

// startupResult is what asking for the task to be on or off came to: nil when
// it is now as asked, else why not.
func startupResult(want bool, got startupTaskState) error {
	if got.on() == want {
		return nil
	}
	switch got {
	case taskDisabledByUser:
		return errStartupInSettings
	case taskDisabledByPolicy, taskEnabledByPolicy:
		return errStartupByPolicy
	}
	return errors.New("Windows did not change it")
}
