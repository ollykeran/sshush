package server

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func writeLog(t *testing.T, l *LogFile, s string) {
	t.Helper()
	if _, err := io.WriteString(l, s); err != nil {
		t.Fatalf("write %q: %v", s, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLogFile_AppendsAcrossReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	for _, line := range []string{"one\n", "two\n"} {
		l, err := OpenLogFile(path, 0)
		if err != nil {
			t.Fatal(err)
		}
		writeLog(t, l, line)
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if got := readFile(t, path); got != "one\ntwo\n" {
		t.Errorf("log = %q, want both runs' lines", got)
	}
}

func TestLogFile_IsPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "sshush")
	path := filepath.Join(dir, "server.log")
	l, err := OpenLogFile(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("log file mode = %v (%v), want 600", info.Mode().Perm(), err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("log directory mode = %v (%v), want 700", info.Mode().Perm(), err)
	}
}

func TestLogFile_RotatesPastItsLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	l, err := OpenLogFile(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	writeLog(t, l, "12345678\n")
	writeLog(t, l, "abcdefgh\n")
	if got := readFile(t, path+".1"); got != "12345678\n" {
		t.Errorf("after the first rotation, .1 = %q", got)
	}
	if got := readFile(t, path); got != "abcdefgh\n" {
		t.Errorf("after the first rotation, log = %q", got)
	}

	writeLog(t, l, "x\n")
	if got := readFile(t, path+".1"); got != "abcdefgh\n" {
		t.Errorf("after the second rotation, .1 = %q, want the newer copy to replace the older", got)
	}
	if got := readFile(t, path); got != "x\n" {
		t.Errorf("after the second rotation, log = %q", got)
	}
}

func TestLogFile_CountsWhatWasAlreadyThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	if err := os.WriteFile(path, []byte("123456789\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := OpenLogFile(path, 12)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	writeLog(t, l, "abc\n")
	if got := readFile(t, path+".1"); got != "123456789\n" {
		t.Errorf(".1 = %q, want the earlier run's log rotated out", got)
	}
}

func TestLogFile_KeepsAnOversizedWriteWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	l, err := OpenLogFile(path, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	writeLog(t, l, "longer than four\n")
	if got := readFile(t, path); got != "longer than four\n" {
		t.Errorf("log = %q, want the whole line", got)
	}
	if _, err := os.Stat(path + ".1"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("an empty log was rotated for an oversized write (%v)", err)
	}
}

func TestLogFile_RefusesWritesAfterClose(t *testing.T) {
	l, err := OpenLogFile(filepath.Join(t.TempDir(), "server.log"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(l, "late\n"); !errors.Is(err, os.ErrClosed) {
		t.Errorf("write after close = %v, want os.ErrClosed", err)
	}
}
