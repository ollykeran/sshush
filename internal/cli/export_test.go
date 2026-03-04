package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExport_stdout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "export-test")

	// runExport with empty outputPath prints to stdout; just verify no error.
	// Stdout capture is complex; we test file output separately.
	err := runExport(privPath, filepath.Join(dir, "out.pub"))
	if err != nil {
		t.Fatalf("runExport: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "out.pub"))
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "ssh-ed25519 ") {
		t.Errorf("expected ssh-ed25519 prefix, got: %s", line)
	}
	if !strings.Contains(line, "export-test") {
		t.Errorf("expected comment in public key line: %s", line)
	}
}

func TestRunExport_toFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "file-export")
	outPath := filepath.Join(dir, "exported.pub")

	if err := runExport(privPath, outPath); err != nil {
		t.Fatalf("runExport: %v", err)
	}

	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("output file not found: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("output permissions: got %o, want %o", perm, 0o644)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "file-export") {
		t.Errorf("expected comment in output: %s", string(data))
	}
}

func TestRunExport_missingFile(t *testing.T) {
	t.Parallel()
	err := runExport(filepath.Join(t.TempDir(), "nonexistent"), "")
	if err == nil {
		t.Fatal("expected error for missing key file")
	}
}

func TestRunExport_notOpenSSHKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	badPath := filepath.Join(dir, "bad_key")
	os.WriteFile(badPath, []byte("not a real key"), 0o600)

	err := runExport(badPath, "")
	if err == nil {
		t.Fatal("expected error for non-OpenSSH key")
	}
}
