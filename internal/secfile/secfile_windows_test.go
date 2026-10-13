package secfile

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// A file created the ordinary way inside a private directory inherits its list.
func TestMkdirAll_childrenInheritOnWindows(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := MkdirAll(dir); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(child, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustBePrivate(t, child)
}

// A file private where it was written stays private when renamed elsewhere,
// which is how the vault is saved (written beside itself, then moved into place).
func TestWriteFile_staysPrivateAcrossRename(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "vault.json.tmp")
	if err := WriteFile(tmp, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(dir, "vault.json")
	if err := os.Rename(tmp, final); err != nil {
		t.Fatal(err)
	}
	mustBePrivate(t, final)
}

// openToEveryone gives path an access list that lets any account read it, the
// state a file is in when its folder is shared. A test's temp directory sits in
// the user's profile, where an ordinary file is private already, so the open
// list has to be put there on purpose.
func openToEveryone(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if private, err := IsPrivate(path); err != nil || private {
		t.Fatalf("%s should be open to everyone now (private %v, %v)", path, private, err)
	}
}

// A new file must be private even where its folder would hand it an open
// list: it is created with its own rather than inheriting and being fixed up.
func TestWriteFile_newFileIsPrivateInAnOpenFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	err = windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "secret")
	if err := WriteFile(path, []byte("hunter2")); err != nil {
		t.Fatal(err)
	}
	mustBePrivate(t, path)
	if got, err := os.ReadFile(path); err != nil || string(got) != "hunter2" {
		t.Fatalf("read back %q, %v", got, err)
	}

	fresh := filepath.Join(dir, "fresh")
	if err := WriteNewFile(fresh, []byte("hunter2")); err != nil {
		t.Fatal(err)
	}
	mustBePrivate(t, fresh)

	sub := filepath.Join(dir, "sub")
	if err := MkdirAll(sub); err != nil {
		t.Fatal(err)
	}
	mustBePrivate(t, sub)
}

// A file that was there already, readable by everyone, is private once
// WriteFile has replaced it, and holds only the new content.
func TestWriteFile_existingOpenFileBecomesPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("old and longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	openToEveryone(t, path)
	if err := WriteFile(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	mustBePrivate(t, path)
	if got, err := os.ReadFile(path); err != nil || string(got) != "new" {
		t.Fatalf("read back %q, %v; want the old content replaced", got, err)
	}
}
