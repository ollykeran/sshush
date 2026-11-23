package tray

import (
	"errors"
	"fmt"
	goruntime "runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Installed from the Microsoft Store, the tray runs inside an MSIX package.
// Its registry writes then go to the package's private copy, so a Run key
// entry starts nothing; the package declares a StartupTask on the tray's
// application instead, which these functions switch through the Windows
// Runtime's StartupTask class. Windows looks a task up by the application
// asking, so the task has to be on the same application the Start menu
// launches, and the tray tells a sign-in start from that one by how Windows
// activated it (startedAtSignIn).
//
// The interfaces are called through their vtables, as the rest of the tray
// calls Win32: the order and IIDs are those of Windows.ApplicationModel in
// the Windows SDK metadata.

var (
	kernel32                      = windows.NewLazySystemDLL("kernel32.dll")
	procGetCurrentPackageFullName = kernel32.NewProc("GetCurrentPackageFullName")

	combase                     = windows.NewLazySystemDLL("combase.dll")
	procRoInitialize            = combase.NewProc("RoInitialize")
	procRoGetActivationFactory  = combase.NewProc("RoGetActivationFactory")
	procWindowsCreateString     = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString     = combase.NewProc("WindowsDeleteString")
	iidIStartupTaskStatics      = windows.GUID{Data1: 0xee5b60bd, Data2: 0xa148, Data3: 0x41a7, Data4: [8]byte{0xb2, 0x6e, 0xe8, 0xb8, 0x8a, 0x1e, 0x62, 0xf8}}
	iidIAppInstanceStatics      = windows.GUID{Data1: 0x9d11e77f, Data2: 0x9ea6, Data3: 0x47af, Data4: [8]byte{0xa6, 0xec, 0x46, 0x78, 0x4c, 0x5b, 0xa2, 0x54}}
	iidIAsyncInfo               = windows.GUID{Data1: 0x00000036, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	errAppModelNoPackage        = syscall.Errno(15700) // APPMODEL_ERROR_NO_PACKAGE
	startupTaskOperationTimeout = 10 * time.Second
)

// Vtable slots. Every WinRT interface starts with IUnknown's three methods
// and IInspectable's three.
const (
	slotQueryInterface = 0
	slotRelease        = 2

	slotStaticsGetAsync = 7 // IStartupTaskStatics: GetForCurrentPackageAsync, GetAsync

	slotAppInstanceGetActivatedEventArgs = 7 // IAppInstanceStatics: RecommendedInstance, GetActivatedEventArgs, ...
	slotActivatedEventArgsKind           = 6 // IActivatedEventArgs: Kind, PreviousExecutionState, SplashScreen

	activationKindStartupTask = 1020 // Windows.ApplicationModel.Activation.ActivationKind.StartupTask

	slotTaskRequestEnableAsync = 6 // IStartupTask: RequestEnableAsync, Disable, State, TaskId
	slotTaskDisable            = 7
	slotTaskState              = 8

	slotAsyncInfoStatus    = 7 // IAsyncInfo: Id, Status, ErrorCode, Cancel, Close
	slotAsyncInfoErrorCode = 8
	slotAsyncOpGetResults  = 8 // IAsyncOperation<T>: Completed (set), Completed (get), GetResults
)

// AsyncStatus values.
const (
	asyncStarted   = 0
	asyncCompleted = 1
)

// packaged reports whether this process runs inside an MSIX package.
func packaged() bool {
	var n uint32
	r, _, _ := procGetCurrentPackageFullName.Call(uintptr(unsafe.Pointer(&n)), 0)
	return syscall.Errno(r) != errAppModelNoPackage
}

// comObject holds a COM interface pointer, whose first word points at its
// vtable. It is the pointer and nothing else, so &obj can be handed to a
// method that returns an interface pointer through an out parameter.
type comObject struct{ p unsafe.Pointer }

func (o comObject) call(slot int, args ...uintptr) error {
	vtbl := *(**[16]uintptr)(o.p)
	all := append([]uintptr{uintptr(o.p)}, args...)
	r, _, _ := syscall.SyscallN(vtbl[slot], all...)
	if int32(r) < 0 {
		return fmt.Errorf("HRESULT 0x%08x", uint32(r))
	}
	return nil
}

func (o comObject) release() {
	if o.p != nil {
		_ = o.call(slotRelease)
	}
}

// await waits for a WinRT IAsyncOperation and returns what GetResults puts in
// result. It polls IAsyncInfo rather than registering a completion delegate,
// which would mean implementing a COM object here; the operations it waits for
// finish in well under a second.
func await(op comObject, result unsafe.Pointer) error {
	var info comObject
	if err := op.call(slotQueryInterface, uintptr(unsafe.Pointer(&iidIAsyncInfo)), uintptr(unsafe.Pointer(&info))); err != nil {
		return fmt.Errorf("IAsyncInfo: %w", err)
	}
	defer info.release()
	deadline := time.Now().Add(startupTaskOperationTimeout)
	for {
		var status int32
		if err := info.call(slotAsyncInfoStatus, uintptr(unsafe.Pointer(&status))); err != nil {
			return err
		}
		switch status {
		case asyncStarted:
			if time.Now().After(deadline) {
				return errors.New("no answer from Windows in time")
			}
			time.Sleep(10 * time.Millisecond)
			continue
		case asyncCompleted:
			return op.call(slotAsyncOpGetResults, uintptr(result))
		}
		var code int32
		_ = info.call(slotAsyncInfoErrorCode, uintptr(unsafe.Pointer(&code)))
		return fmt.Errorf("the operation failed (HRESULT 0x%08x)", uint32(code))
	}
}

// winrtCalls carries work to the one thread that calls the Windows Runtime.
var (
	winrtCalls     = make(chan func())
	winrtStartOnce sync.Once
)

// onWinRTThread runs f on a thread of its own in the multithreaded
// apartment, so polling an operation never ties up the window thread or
// depends on it pumping messages. The thread joins the apartment once and
// stays for the life of the process: leaving it after each call tears the
// Windows Runtime down, and the next call can find it still stopping
// (CO_E_SERVER_STOPPING).
func onWinRTThread(f func() error) error {
	winrtStartOnce.Do(func() {
		go func() {
			goruntime.LockOSThread()
			const roInitMultithreaded = 1
			procRoInitialize.Call(roInitMultithreaded)
			for call := range winrtCalls {
				call()
			}
		}()
	})
	done := make(chan error, 1)
	winrtCalls <- func() { done <- f() }
	return <-done
}

// activationFactory returns the factory for the Windows Runtime class name,
// as the interface iid.
func activationFactory(name string, iid *windows.GUID) (comObject, error) {
	var factory comObject
	className, err := hstring(name)
	if err != nil {
		return factory, err
	}
	defer procWindowsDeleteString.Call(className)
	if r, _, _ := procRoGetActivationFactory.Call(className, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&factory))); int32(r) < 0 {
		return factory, fmt.Errorf("%s: HRESULT 0x%08x", name, uint32(r))
	}
	return factory, nil
}

// StartedAtSignIn reports whether Windows started this process for the
// package's StartupTask, rather than from the Start menu or a shell. It is
// false outside a package, where the Run key passes --background instead.
func StartedAtSignIn() bool {
	if !packaged() {
		return false
	}
	var atSignIn bool
	_ = onWinRTThread(func() error {
		statics, err := activationFactory("Windows.ApplicationModel.AppInstance", &iidIAppInstanceStatics)
		if err != nil {
			return err
		}
		defer statics.release()
		var args comObject
		if err := statics.call(slotAppInstanceGetActivatedEventArgs, uintptr(unsafe.Pointer(&args))); err != nil {
			return err
		}
		if args.p == nil {
			return nil // started some other way than an activation, e.g. from a shell
		}
		defer args.release()
		var kind int32
		if err := args.call(slotActivatedEventArgsKind, uintptr(unsafe.Pointer(&kind))); err != nil {
			return err
		}
		atSignIn = kind == activationKindStartupTask
		return nil
	})
	return atSignIn
}

// withStartupTask runs f with the package's StartupTask.
func withStartupTask(f func(task comObject) error) error {
	return onWinRTThread(func() error { return startupTask(f) })
}

func startupTask(f func(task comObject) error) error {
	statics, err := activationFactory("Windows.ApplicationModel.StartupTask", &iidIStartupTaskStatics)
	if err != nil {
		return err
	}
	defer statics.release()

	id, err := hstring(startupTaskID)
	if err != nil {
		return err
	}
	defer procWindowsDeleteString.Call(id)
	var op comObject
	if err := statics.call(slotStaticsGetAsync, id, uintptr(unsafe.Pointer(&op))); err != nil {
		return fmt.Errorf("StartupTask %s: %w", startupTaskID, err)
	}
	defer op.release()
	var task comObject
	if err := await(op, unsafe.Pointer(&task)); err != nil {
		return fmt.Errorf("StartupTask %s: %w", startupTaskID, err)
	}
	defer task.release()
	return f(task)
}

func hstring(s string) (uintptr, error) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h uintptr
	if r, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h))); int32(r) < 0 {
		return 0, fmt.Errorf("WindowsCreateString: HRESULT 0x%08x", uint32(r))
	}
	return h, nil
}

// openStartupSettings opens Settings > Apps > Startup, the only place a
// StartupTask the user turned off can be turned back on.
func openStartupSettings() {
	verb, _ := windows.UTF16PtrFromString("open")
	uri, _ := windows.UTF16PtrFromString("ms-settings:startupapps")
	_ = windows.ShellExecute(0, verb, uri, nil, nil, windows.SW_SHOWNORMAL)
}

// packagedStartupState is the StartupTask's state.
func packagedStartupState() (startupTaskState, error) {
	var state startupTaskState
	err := withStartupTask(func(task comObject) error {
		return task.call(slotTaskState, uintptr(unsafe.Pointer(&state)))
	})
	return state, err
}

// setPackagedStartup asks Windows to turn the StartupTask on or off. Turning
// it on is a request: Windows refuses when the user turned it off in Settings
// or policy decides, and startupResult says which.
func setPackagedStartup(on bool) error {
	var state startupTaskState
	err := withStartupTask(func(task comObject) error {
		if on {
			var op comObject
			if err := task.call(slotTaskRequestEnableAsync, uintptr(unsafe.Pointer(&op))); err != nil {
				return err
			}
			defer op.release()
			return await(op, unsafe.Pointer(&state))
		}
		if err := task.call(slotTaskDisable); err != nil {
			return err
		}
		return task.call(slotTaskState, uintptr(unsafe.Pointer(&state)))
	})
	if err != nil {
		return err
	}
	return startupResult(on, state)
}
