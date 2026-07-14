package editcomment

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEditCommentWithEditor_Changed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on windows")
	}
	editor := writeScript(t, "editor.sh", "#!/bin/sh\nprintf 'new comment' > \"$1\"\n")
	got, err := EditCommentWithEditor("old", editor)
	if err != nil {
		t.Fatalf("EditCommentWithEditor: %v", err)
	}
	if got != "new comment" {
		t.Errorf("got %q, want %q", got, "new comment")
	}
}

func TestEditCommentWithEditor_Unchanged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on windows")
	}
	editor := writeScript(t, "noop.sh", "#!/bin/sh\nexit 0\n")
	_, err := EditCommentWithEditor("keep", editor)
	if err != ErrExitedWithoutSaving {
		t.Errorf("got err = %v, want ErrExitedWithoutSaving", err)
	}
}

func TestEditCommentWithEditor_InvalidEditor(t *testing.T) {
	_, err := EditCommentWithEditor("x", "")
	if err == nil {
		t.Error("empty editor should error")
	}
}

func TestEditCommentWithEditor_MissingEditor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on windows")
	}
	_, err := EditCommentWithEditor("x", "/nonexistent/editor")
	if err == nil {
		t.Error("missing editor should error")
	}
}

func writeScript(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEditCommentWithEditor_EditorFailed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on windows")
	}
	editor := writeScript(t, "fail.sh", "#!/bin/sh\nexit 1\n")
	_, err := EditCommentWithEditor("x", editor)
	if err == nil {
		t.Error("editor exit 1 should error")
	}
}

func TestEditCommentWithEditor_TrimmedOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on windows")
	}
	editor := writeScript(t, "trim.sh", "#!/bin/sh\nprintf '  new  ' > \"$1\"\n")
	got, err := EditCommentWithEditor("old", editor)
	if err != nil {
		t.Fatalf("EditCommentWithEditor: %v", err)
	}
	if got != "new" {
		t.Errorf("got %q, want %q", got, "new")
	}
}

func TestEditCommentWithEditor_WhitespaceOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on windows")
	}
	editor := writeScript(t, "ws.sh", "#!/bin/sh\nprintf '   ' > \"$1\"\n")
	got, err := EditCommentWithEditor("old", editor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("whitespace-only trimmed to empty, got %q", got)
	}
}

func init() {
	// Ensure exec.LookPath can find /bin/sh
	if _, err := exec.LookPath("sh"); err != nil {
		panic("sh not found: " + err.Error())
	}
}
