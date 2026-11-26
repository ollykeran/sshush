package pageant

import (
	"encoding/binary"
	"fmt"
	"io"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// askPageant does what a PuTTY-family client does: puts req in a named file
// mapping, sends the Pageant window WM_COPYDATA naming it, and reads the reply
// back out of the mapping.
func askPageant(t *testing.T, req []byte) []byte {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	class, _ := windows.UTF16PtrFromString("Pageant")
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(class)), 0)
	if hwnd == 0 {
		t.Fatal("no Pageant window")
	}
	name := fmt.Sprintf("PageantRequest%08x", windows.GetCurrentThreadId())
	namePtr, _ := windows.UTF16PtrFromString(name)
	mapping, err := windows.CreateFileMapping(windows.InvalidHandle, nil, windows.PAGE_READWRITE, 0, MaxMessage, namePtr)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(mapping)
	view, err := windows.MapViewOfFile(mapping, windows.FILE_MAP_WRITE, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.UnmapViewOfFile(view)
	// view is the address MapViewOfFile returned; read it as a pointer without
	// a uintptr-to-pointer conversion vet would flag.
	mem := unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&view))), MaxMessage)
	copy(mem, req)

	ascii := append([]byte(name), 0)
	cds := struct {
		dwData uintptr
		cbData uint32
		lpData uintptr
	}{0x804e50ba, uint32(len(ascii)), uintptr(unsafe.Pointer(&ascii[0]))}
	const wmCopyData = 0x004A
	r, _, _ := user32.NewProc("SendMessageW").Call(hwnd, wmCopyData, 0, uintptr(unsafe.Pointer(&cds)))
	runtime.KeepAlive(ascii)
	if r == 0 {
		t.Fatal("Pageant window refused the request")
	}
	n := binary.BigEndian.Uint32(mem)
	return append([]byte(nil), mem[:4+n]...)
}

// The window can only be served once per process and never taken down, so this
// is the one test that does.
func TestServe_answersAPageantClient(t *testing.T) {
	if windowExists() {
		t.Skip("another program already answers as Pageant on this machine")
	}
	if got := Serve(keyringWith(t, 3, "pageant"), io.Discard); got != Serving {
		t.Fatalf("Serve = %v, want Serving", got)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if pid, ok := OwnerPID(); ok {
			if pid != windows.GetCurrentProcessId() {
				t.Fatalf("Pageant window belongs to pid %d", pid)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the Pageant window never appeared")
		}
		time.Sleep(20 * time.Millisecond)
	}

	reply := askPageant(t, requestIdentities)
	if reply[4] != 12 {
		t.Fatalf("reply type %d, want 12", reply[4])
	}
	if n := binary.BigEndian.Uint32(reply[5:]); n != 3 {
		t.Fatalf("%d keys, want 3", n)
	}

	// With the window taken, a second Serve leaves it be.
	if got := Serve(keyringWith(t, 1, ""), io.Discard); got != Taken {
		t.Fatalf("second Serve = %v, want Taken", got)
	}
}
