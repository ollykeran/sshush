package utils

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// TestDiscoverKeyPaths_searchDirs checks that keys are found in every given
// directory, and that missing ones and plain files are skipped quietly.
func TestDiscoverKeyPaths_searchDirs(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	keyA, keyB := filepath.Join(a, "work_key"), filepath.Join(b, "personal")
	mustWriteTestKey(t, keyA)
	mustWriteTestKey(t, keyB)
	missing := filepath.Join(t.TempDir(), "gone")

	dirs := []string{missing, a, keyA, b}
	paths := DiscoverKeyPaths(dirs, false, false, false)
	got := make([]string, len(paths))
	for i, p := range paths {
		got[i] = p.Path
	}
	if want := []string{keyA, keyB}; !reflect.DeepEqual(got, want) {
		t.Errorf("found %v, want %v", got, want)
	}
	if !reflect.DeepEqual(dirs, []string{missing, a, keyA, b}) {
		t.Errorf("DiscoverKeyPaths changed its searchDirs argument: %v", dirs)
	}
}

// TestDiscoverKeyPaths_doesNotWriteThroughCallerSlice guards the copy made
// before ~/.ssh and the current directory are appended: a slice with spare
// capacity (such as a config's search_dirs) must not have them written into it.
func TestDiscoverKeyPaths_doesNotWriteThroughCallerSlice(t *testing.T) {
	backing := make([]string, 1, 4)
	backing[0] = t.TempDir()
	DiscoverKeyPaths(backing, true, true, false)
	if extra := backing[:cap(backing)][1]; extra != "" {
		t.Errorf("caller's backing array was written: %q", extra)
	}
}

func TestUnreadableDirs(t *testing.T) {
	ok := t.TempDir()
	missing := filepath.Join(ok, "missing")
	file := filepath.Join(ok, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := []string{missing, file}
	if runtime.GOOS != "windows" && os.Getuid() != 0 {
		locked := filepath.Join(ok, "locked")
		if err := os.Mkdir(locked, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
		want = append(want, locked)
	}
	dirs := append([]string{ok}, want...)
	if got := UnreadableDirs(dirs); !reflect.DeepEqual(got, want) {
		t.Errorf("UnreadableDirs = %v, want %v", got, want)
	}
	if got := UnreadableDirs(nil); got != nil {
		t.Errorf("UnreadableDirs(nil) = %v, want nil", got)
	}
}
