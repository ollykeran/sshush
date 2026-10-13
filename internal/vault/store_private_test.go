package vault

import (
	"path/filepath"
	"testing"

	"github.com/ollykeran/sshush/internal/secfile"
)

// The vault file holds every key: it, and a directory made to hold it, must be
// readable by the current user only — on Windows too, where that takes an
// access list rather than a file mode.
func TestSave_vaultFileAndNewDirectoryArePrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	path := filepath.Join(dir, "vault.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	// Saved twice: the second save replaces a file that already exists.
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{dir, path} {
		private, err := secfile.IsPrivate(p)
		if err != nil {
			t.Fatal(err)
		}
		if !private {
			t.Errorf("%s is readable by other users", p)
		}
	}
}
