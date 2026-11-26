package pageant

import (
	"fmt"
	"io"
	"time"
	"unsafe"

	winpageant "github.com/ndbeals/winssh-pageant/pageant"
	sshagent "golang.org/x/crypto/ssh/agent"
	"golang.org/x/sys/windows"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procFindWindowW              = user32.NewProc("FindWindowW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
)

// windowExists reports whether some process already has a Pageant window. A
// var so tests can pretend one does.
var windowExists = func() bool {
	_, ok := OwnerPID()
	return ok
}

// OwnerPID returns the process that has the Pageant window, if any process
// does.
func OwnerPID() (uint32, bool) {
	class, err := windows.UTF16PtrFromString("Pageant")
	if err != nil {
		return 0, false
	}
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(class)), 0)
	if hwnd == 0 {
		return 0, false
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid, pid != 0
}

// Serve makes this process answer as Pageant with ext, for as long as the
// process lives: the window cannot be taken down again, which is fine for the
// daemon, whose restart is a new process. It says on w when another program
// already answers as Pageant, and leaves that program be.
//
// Only the window is served, not Pageant's named pipe: every Pageant client
// falls back to the window, and the library's pipe takes requests of any size
// from anyone.
func Serve(ext sshagent.Agent, w io.Writer) Status {
	if windowExists() {
		fmt.Fprintln(w, "sshushd: another program already answers as Pageant; leaving PuTTY-family programs to it")
		return Taken
	}
	keepStdout()
	p := winpageant.NewWithOptions(
		winpageant.WithPageantPipe(false),
		winpageant.WithPageantRequestHandler(func(_ *winpageant.Pageant, req []byte) ([]byte, error) {
			return Exchange(ext, req), nil
		}),
	)
	go p.Run()
	// Run makes the window on its own goroutine and says nothing when it fails,
	// so wait to see it: whoever started the daemon may ask for Pageant as soon
	// as it hears the agent is up.
	me := windows.GetCurrentProcessId()
	for deadline := time.Now().Add(windowWait); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if pid, ok := OwnerPID(); ok {
			if pid == me {
				return Serving
			}
			fmt.Fprintln(w, "sshushd: another program took the Pageant window first; leaving PuTTY-family programs to it")
			return Taken
		}
	}
	fmt.Fprintln(w, "sshushd: could not create the Pageant window; PuTTY-family programs will not find the agent")
	return Failed
}

// windowWait is how long Serve waits for the window to appear.
const windowWait = 3 * time.Second

// keepStdout gives the process a stdout if it has none. The library's Run
// attaches to the parent's console when there is no stdout, and a daemon
// attached to the console of the terminal that started it would be stopped
// when that terminal closes.
func keepStdout() {
	h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err == nil && h != 0 && h != windows.InvalidHandle {
		return
	}
	nul, err := windows.UTF16PtrFromString("NUL")
	if err != nil {
		return
	}
	f, err := windows.CreateFile(nul, windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return
	}
	_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, f)
}
