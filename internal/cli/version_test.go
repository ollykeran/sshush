package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/version"
)

func TestRunVersion(t *testing.T) {
	t.Parallel()
	cmd := newVersionCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, version.Version) {
		t.Errorf("output missing version %q: %s", version.Version, out)
	}
	if !strings.Contains(out, "sshush") {
		t.Errorf("output missing name %q: %s", "sshush", out)
	}
}

func TestVersionCommand_rejectsArgs(t *testing.T) {
	t.Parallel()
	cmd := newVersionCommand()
	cmd.SetArgs([]string{"extra"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for extra args")
	}
}
