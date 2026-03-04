package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/openssh"
)

func TestRunEdit_commentFlag(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "old-comment")

	err := runEdit(privPath, "", "new-comment", false, "")
	if err != nil {
		t.Fatalf("runEdit: %v", err)
	}

	assertPrivKeyComment(t, privPath, "new-comment")
	assertPubKeyComment(t, privPath+".pub", "new-comment")
}

func TestRunEdit_copyToNewPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "original")
	copyPath := filepath.Join(dir, "copy_key")

	err := runEdit(privPath, "", "copied-comment", true, copyPath)
	if err != nil {
		t.Fatalf("runEdit: %v", err)
	}

	// Original unchanged
	assertPrivKeyComment(t, privPath, "original")
	// Copy has new comment
	assertPrivKeyComment(t, copyPath, "copied-comment")

	if _, err := os.Stat(copyPath + ".pub"); err != nil {
		t.Fatalf("copy .pub not created: %v", err)
	}
	assertPubKeyComment(t, copyPath+".pub", "copied-comment")
}

func TestRunEdit_copyWithoutOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "test")

	err := runEdit(privPath, "", "x", true, "")
	if err == nil {
		t.Fatal("expected error when --copy without --output")
	}
}

func TestRunEdit_outputWithoutCopy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "test")

	err := runEdit(privPath, "", "x", false, "/tmp/somewhere")
	if err == nil {
		t.Fatal("expected error when --output without --copy")
	}
}

func TestRunEdit_missingFile(t *testing.T) {
	t.Parallel()
	err := runEdit(filepath.Join(t.TempDir(), "nonexistent"), "", "x", false, "")
	if err == nil {
		t.Fatal("expected error for missing key file")
	}
}

func TestRunEdit_notOpenSSHKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	badPath := filepath.Join(dir, "bad_key")
	os.WriteFile(badPath, []byte("not a key"), 0o600)

	err := runEdit(badPath, "", "x", false, "")
	if err == nil {
		t.Fatal("expected error for non-OpenSSH key")
	}
}

func TestRunEdit_emptyComment(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "has-comment")
	// Use a fake editor that writes whitespace-only content
	editorPath := writeFakeEditor(t, dir, "empty-editor.sh", "   ")

	err := runEdit(privPath, editorPath, "", false, "")
	if err == nil {
		t.Fatal("expected error for empty comment")
	}
}

func TestRunEdit_pubFileUpdated(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "before")

	err := runEdit(privPath, "", "after", false, "")
	if err != nil {
		t.Fatalf("runEdit: %v", err)
	}

	assertPubKeyComment(t, privPath+".pub", "after")
}

func TestRunEdit_noPubFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "only-priv")
	os.Remove(privPath + ".pub")

	err := runEdit(privPath, "", "updated", false, "")
	if err != nil {
		t.Fatalf("runEdit: %v", err)
	}

	assertPrivKeyComment(t, privPath, "updated")
	if _, err := os.Stat(privPath + ".pub"); !os.IsNotExist(err) {
		t.Error("should not create .pub when it didn't exist before")
	}
}

// --- editor tests ---

func TestEditCommentWithEditor_success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	editorPath := writeFakeEditor(t, dir, "editor.sh", "edited-by-script")

	result, err := editCommentWithEditor("old-comment", editorPath)
	if err != nil {
		t.Fatalf("editCommentWithEditor: %v", err)
	}
	if result != "edited-by-script" {
		t.Errorf("got %q, want %q", result, "edited-by-script")
	}
}

func TestEditCommentWithEditor_editorFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	editorPath := writeFailingEditor(t, dir, "bad-editor.sh")

	_, err := editCommentWithEditor("old-comment", editorPath)
	if err == nil {
		t.Fatal("expected error when editor exits non-zero")
	}
}

func TestResolveEditor_flagTakesPrecedence(t *testing.T) {
	t.Setenv("EDITOR", "nano")
	if got := resolveEditor("custom-editor"); got != "custom-editor" {
		t.Errorf("resolveEditor: got %q, want %q", got, "custom-editor")
	}
}

func TestResolveEditor_envFallback(t *testing.T) {
	t.Setenv("EDITOR", "emacs")
	if got := resolveEditor(""); got != "emacs" {
		t.Errorf("resolveEditor: got %q, want %q", got, "emacs")
	}
}

func TestResolveEditor_defaultVi(t *testing.T) {
	t.Setenv("EDITOR", "")
	if got := resolveEditor(""); got != "vi" {
		t.Errorf("resolveEditor: got %q, want %q", got, "vi")
	}
}

func TestRunEdit_editorFlow(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "old-comment")
	editorPath := writeFakeEditor(t, dir, "editor.sh", "editor-comment")

	// empty commentFlag triggers editor path
	err := runEdit(privPath, editorPath, "", false, "")
	if err != nil {
		t.Fatalf("runEdit with editor: %v", err)
	}

	assertPrivKeyComment(t, privPath, "editor-comment")
}

func TestRunEdit_editorFailsReportsError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "old-comment")
	editorPath := writeFailingEditor(t, dir, "bad-editor.sh")

	err := runEdit(privPath, editorPath, "", false, "")
	if err == nil {
		t.Fatal("expected error when editor fails")
	}
}

// --- assertion helpers ---

func assertPrivKeyComment(t *testing.T, privPath, wantComment string) {
	t.Helper()
	data, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	parsed, err := openssh.ParsePrivateKeyBlob(data)
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}
	if parsed.Comment != wantComment {
		t.Errorf("private key comment: got %q, want %q", parsed.Comment, wantComment)
	}
}

func assertPubKeyComment(t *testing.T, pubPath, wantComment string) {
	t.Helper()
	data, err := os.ReadFile(pubPath)
	if err != nil {
		t.Fatalf("read pub: %v", err)
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasSuffix(line, wantComment) {
		t.Errorf("pub key line should end with %q: %s", wantComment, line)
	}
}
