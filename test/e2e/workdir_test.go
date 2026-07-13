package e2e

import (
	"os"
	"runtime"
	"testing"
)

// e2eWorkDir returns a temp directory with a short path so Unix socket paths fit
// sockaddr_un limits on macOS (t.TempDir() under /var/folders/... is often too long).
func e2eWorkDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	d, err := os.MkdirTemp("/tmp", "sshush-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}
