package sshushd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareServerLog_CreatesAPrivateLog(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "sshush")
	path := filepath.Join(dir, "server.log")
	if err := prepareServerLog(path); err != nil {
		t.Fatalf("prepareServerLog: %v", err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("log file mode = %v (%v), want 600", info.Mode().Perm(), err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("log directory mode = %v (%v), want 700", info.Mode().Perm(), err)
	}
}

func TestPrepareServerLog_KeepsAnExistingLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	if err := os.WriteFile(path, []byte("earlier run\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareServerLog(path); err != nil {
		t.Fatalf("prepareServerLog: %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "earlier run\n" {
		t.Errorf("log = %q, want the earlier run's records kept", data)
	}
}

func TestPrepareServerLog_ReportsAnUnwritablePath(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareServerLog(filepath.Join(blocker, "server.log")); err == nil {
		t.Error("a log under a file, not a directory, was accepted")
	}
}

func TestNewServerLogWriter_RotatesAndKeepsBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	w := newServerLogWriter(path)
	defer w.Close()

	if w.Filename != path || w.MaxSize != serverLogMaxMB || w.MaxBackups != serverLogBackups || !w.Compress {
		t.Errorf("writer = %s at %d MB, %d backups, compress %v; want %s at %d MB, %d compressed backups",
			w.Filename, w.MaxSize, w.MaxBackups, w.Compress, path, serverLogMaxMB, serverLogBackups)
	}
	if _, err := w.Write([]byte("record\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "record") {
		t.Errorf("log = %q, want the record written to %s", data, path)
	}
}
