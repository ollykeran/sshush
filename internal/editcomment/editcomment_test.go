package editcomment

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ollykeran/sshush/internal/editcomment/editortest"
)

func TestEditCommentWithEditor_Changed(t *testing.T) {
	editor := editortest.Script(t, t.TempDir(), 0, "new comment")
	got, err := EditCommentWithEditor("old", editor)
	if err != nil {
		t.Fatalf("EditCommentWithEditor: %v", err)
	}
	if got != "new comment" {
		t.Errorf("got %q, want %q", got, "new comment")
	}
}

func TestEditCommentWithEditor_Unchanged(t *testing.T) {
	editor := editortest.Script(t, t.TempDir(), 0)
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
	_, err := EditCommentWithEditor("x", "/nonexistent/editor")
	if err == nil {
		t.Error("missing editor should error")
	}
}

func TestEditCommentWithEditor_EditorFailed(t *testing.T) {
	editor := editortest.Script(t, t.TempDir(), 1)
	_, err := EditCommentWithEditor("x", editor)
	if err == nil {
		t.Error("editor exit 1 should error")
	}
}

func TestEditCommentWithEditor_TrimmedOutput(t *testing.T) {
	editor := editortest.Script(t, t.TempDir(), 0, "  new  ")
	got, err := EditCommentWithEditor("old", editor)
	if err != nil {
		t.Fatalf("EditCommentWithEditor: %v", err)
	}
	if got != "new" {
		t.Errorf("got %q, want %q", got, "new")
	}
}

func TestEditCommentWithEditor_WhitespaceOnly(t *testing.T) {
	editor := editortest.Script(t, t.TempDir(), 0, "   ")
	got, err := EditCommentWithEditor("old", editor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("whitespace-only trimmed to empty, got %q", got)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		comment string
		wantErr bool
	}{
		{"plain comment", "my-key", false},
		{"empty comment", "", false},
		{"newline", "foo\nbar", true},
		{"carriage return", "foo\rbar", true},
		{"crlf", "foo\r\nbar", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.comment)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate(%q) error = %v, wantErr %v", tt.comment, err, tt.wantErr)
			}
		})
	}
}

func TestEditCommentWithEditor_RejectsNewline(t *testing.T) {
	editor := editortest.Script(t, t.TempDir(), 0, "line one", "line two")
	_, err := EditCommentWithEditor("old", editor)
	if err == nil {
		t.Fatal("expected error for multi-line editor output")
	}
}

func TestEditorCommand(t *testing.T) {
	// A real program at a path with a space in it, as "C:\Program Files\..." has.
	spaced := filepath.Join(t.TempDir(), "my editor")
	if err := os.MkdirAll(spaced, 0o755); err != nil {
		t.Fatal(err)
	}
	program := editortest.Script(t, spaced, 0)

	tests := []struct {
		name, editor string
		wantProgram  string
		wantArgs     []string
		wantErr      bool
	}{
		{"bare name", "vim", "vim", nil, false},
		{"name with flags", "code --wait", "code", []string{"--wait"}, false},
		{"extra spaces", "  nano   -w  ", "nano", []string{"-w"}, false},
		{"unquoted path with a space", program, program, nil, false},
		{"double-quoted path with flags", `"` + program + `" --wait`, program, []string{"--wait"}, false},
		{"single-quoted path", "'" + program + "' -n", program, []string{"-n"}, false},
		{"quoted argument", `vim -c "set tw=0"`, "vim", []string{"-c", "set tw=0"}, false},
		{"backslashes are kept", `C:\tools\ed.exe -x`, `C:\tools\ed.exe`, []string{"-x"}, false},
		{"empty", "", "", nil, true},
		{"only spaces", "   ", "", nil, true},
		{"unterminated quote", `"vim --wait`, "", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program, args, err := editorCommand(tt.editor)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if program != tt.wantProgram || !reflect.DeepEqual(args, tt.wantArgs) {
				t.Fatalf("got %q %q, want %q %q", program, args, tt.wantProgram, tt.wantArgs)
			}
		})
	}
}

// The point of the quoting rules: an editor installed under a path with a
// space in it can be run.
func TestEditCommentWithEditor_EditorPathWithSpace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Program Files")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	editor := editortest.Script(t, dir, 0, "new comment")
	for _, setting := range []string{editor, `"` + editor + `"`} {
		got, err := EditCommentWithEditor("old", setting)
		if err != nil {
			t.Fatalf("editor %s: %v", setting, err)
		}
		if got != "new comment" {
			t.Errorf("editor %s: got %q", setting, got)
		}
	}
}
