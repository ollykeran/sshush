package vault

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveToFile returns the path to the vault JSON file. Path must already be
// expanded (e.g. home directory expanded). If path is a directory or ends with
// a separator, returns path/vault.json; otherwise returns path unchanged.
func ResolveToFile(path string) string {
	if path == "" {
		return path
	}
	if strings.HasSuffix(path, string(filepath.Separator)) {
		return filepath.Join(path, "vault.json")
	}
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		return filepath.Join(path, "vault.json")
	}
	return path
}
