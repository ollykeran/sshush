package vault

import (
	"path/filepath"
	"testing"
)

// TestOpen_MissingFile verifies that Open returns an empty store when the file does not exist.
func TestOpen_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if store.GetMetadata() != nil {
		t.Error("expected nil metadata for new store")
	}
	if len(store.AllIdentities()) != 0 {
		t.Error("expected no identities for new store")
	}
}
