package transport

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// socketDir returns a directory short enough for a socket path to fit
// sockaddr_un, which t.TempDir() under a long TMPDIR may not be.
func socketDir(t *testing.T) string {
	t.Helper()
	base := "/tmp"
	if runtime.GOOS == "windows" {
		// %TEMP%, not t.TempDir(): that adds the test's name, and Windows has the
		// same limit on a socket path's length.
		base = ""
	}
	dir, err := os.MkdirTemp(base, "sshush-tr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// echoOnce checks that bytes written by a client dialling path reach the listener.
func echoOnce(t *testing.T, path string) {
	t.Helper()
	l, err := Listen(path)
	if err != nil {
		t.Fatalf("Listen(%q): %v", path, err)
	}
	defer l.Close()

	got := make(chan string, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			got <- "accept: " + err.Error()
			return
		}
		defer conn.Close()
		data, _ := io.ReadAll(conn)
		got <- string(data)
	}()

	conn, err := Dial(path)
	if err != nil {
		t.Fatalf("Dial(%q): %v", path, err)
	}
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if s := <-got; s != "hello" {
		t.Fatalf("listener read %q, want %q", s, "hello")
	}
}

func TestSocket_roundTrip(t *testing.T) {
	// The directory does not exist yet: Listen creates it.
	path := filepath.Join(socketDir(t), "sub", "agent.sock")
	if IsPipe(path) {
		t.Fatalf("IsPipe(%q) = true", path)
	}
	echoOnce(t, path)
}

func TestSocket_listenReplacesStaleFile(t *testing.T) {
	path := filepath.Join(socketDir(t), "agent.sock")
	if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	echoOnce(t, path)
}

func TestSocket_removeDeletesFile(t *testing.T) {
	path := filepath.Join(socketDir(t), "agent.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	Remove(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be gone, stat err = %v", path, err)
	}
}

func TestDial_nothingListening(t *testing.T) {
	if _, err := Dial(filepath.Join(socketDir(t), "nobody.sock")); err == nil {
		t.Fatal("expected an error dialling a socket nobody serves")
	}
}

func TestAbs_socketBecomesAbsolute(t *testing.T) {
	got, err := Abs("agent.sock")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("Abs = %q, want an absolute path", got)
	}
}
