package secfile

import (
	"os"
	"path/filepath"
	"testing"
)

func mustBePrivate(t *testing.T, path string) {
	t.Helper()
	private, err := IsPrivate(path)
	if err != nil {
		t.Fatal(err)
	}
	if !private {
		t.Fatalf("%s can be read by other users", path)
	}
}

func TestWriteFile_isPrivateAndHoldsTheData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := WriteFile(path, []byte("hunter2")); err != nil {
		t.Fatal(err)
	}
	mustBePrivate(t, path)
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "hunter2" {
		t.Fatalf("read back %q, %v", got, err)
	}
}

// A file that was already there, open to others, must not stay that way.
func TestWriteFile_closesAnExistingOpenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("old and longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if private, err := IsPrivate(path); err != nil || private {
		t.Skipf("an ordinary file here is already private (%v, %v): nothing to tighten", private, err)
	}
	if err := WriteFile(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	mustBePrivate(t, path)
	if got, _ := os.ReadFile(path); string(got) != "new" {
		t.Fatalf("read back %q, want the old content replaced", got)
	}
}

func TestMkdirAll_newDirectoryIsPrivateAndPassesItOn(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	if err := MkdirAll(dir); err != nil {
		t.Fatal(err)
	}
	mustBePrivate(t, dir)
	// Nothing sshush writes there may be more open than the directory, even
	// when written without this package.
	if err := MkdirAll(dir); err != nil {
		t.Fatalf("second MkdirAll: %v", err)
	}
}

func TestMkdirAll_leavesAnExistingDirectoryAlone(t *testing.T) {
	dir := t.TempDir()
	before, err := IsPrivate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := MkdirAll(dir); err != nil {
		t.Fatal(err)
	}
	if after, _ := IsPrivate(dir); after != before {
		t.Fatalf("existing directory changed: private %v -> %v", before, after)
	}
}

func TestMkdirAll_refusesAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MkdirAll(path); err == nil {
		t.Fatal("expected an error for a path that is a file")
	}
}

func TestRestrict_missingPath(t *testing.T) {
	if err := Restrict(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected an error")
	}
}
