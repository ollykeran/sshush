package secfile

import (
	"os"
	"path/filepath"
	"testing"
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
