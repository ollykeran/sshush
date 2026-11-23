package tray

import (
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"
	"unsafe"

	"fyne.io/systray"
	"golang.org/x/sys/windows"
)

// One tray per logon session. A new one always replaces the old: while the
// tray changes quickly, starting a fresh build should never leave the last
// one behind. Local\ names are per logon session.
const (
	instanceMutexName = `Local\sshush-tray`
	quitEventName     = `Local\sshush-tray-quit`
	// graceTimeout is how long an earlier tray has to quit when asked before
	// it is terminated.
	graceTimeout = 3 * time.Second
	// exitTimeout is how long this tray gives itself to close its icon on
	// being replaced before it simply exits.
	exitTimeout = 2 * time.Second
)

// TakeOver makes this the only sshush-tray in the logon session. An earlier
// one is asked to quit and, if it does not in time, terminated, as is any
// tray from before this handover existed. It then watches for the next tray
// to ask the same of it.
func TakeOver() error {
	result := make(chan error, 1)
	go func() {
		// A mutex belongs to the thread that acquired it. This one keeps the
		// thread, so the mutex is held until the process ends, and is then
		// abandoned, which the next tray takes as released.
		goruntime.LockOSThread()

		mutexName, _ := windows.UTF16PtrFromString(instanceMutexName)
		mutex, err := windows.CreateMutex(nil, false, mutexName)
		if mutex == 0 {
			result <- err
			return
		}
		existed := errors.Is(err, windows.ERROR_ALREADY_EXISTS)
		eventName, _ := windows.UTF16PtrFromString(quitEventName)
		quit, err := windows.CreateEvent(nil, 0, 0, eventName) // auto-reset
		if quit == 0 {
			result <- err
			return
		}
		if existed {
			_ = windows.SetEvent(quit)
		}
		if !acquire(mutex, graceTimeout) {
			terminateOtherTrays()
			if !acquire(mutex, graceTimeout) {
				result <- errors.New("an earlier sshush-tray did not quit and could not be stopped")
				return
			}
		}
		// The signal was for the tray this one replaced; if it died before
		// taking it, it must not make this one quit.
		_ = windows.ResetEvent(quit)
		// A tray from before the handover never heard the signal.
		terminateOtherTrays()
		result <- nil

		_, _ = windows.WaitForSingleObject(quit, windows.INFINITE)
		systray.Quit()
		time.Sleep(exitTimeout)
		os.Exit(0)
	}()
	return <-result
}

// acquire waits up to timeout to own mutex. An abandoned mutex, left by a
// tray that exited, counts as acquired.
func acquire(mutex windows.Handle, timeout time.Duration) bool {
	r, _ := windows.WaitForSingleObject(mutex, uint32(timeout.Milliseconds()))
	return r == windows.WAIT_OBJECT_0 || r == windows.WAIT_ABANDONED
}

// terminateOtherTrays ends every other process in this logon session running
// an executable with this one's name.
func terminateOtherTrays() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	name := filepath.Base(self)
	myPID := windows.GetCurrentProcessId()
	var mySession uint32
	if err := windows.ProcessIdToSessionId(myPID, &mySession); err != nil {
		return
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if entry.ProcessID == myPID || !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), name) {
			continue
		}
		var session uint32
		if windows.ProcessIdToSessionId(entry.ProcessID, &session) != nil || session != mySession {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, entry.ProcessID)
		if err != nil {
			continue
		}
		if windows.TerminateProcess(h, 1) == nil {
			_, _ = windows.WaitForSingleObject(h, uint32(graceTimeout.Milliseconds()))
		}
		_ = windows.CloseHandle(h)
	}
}
