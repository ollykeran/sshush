// Package editcomment runs an external editor on a key comment in a temp file.
// Shared by CLI and GUI so behaviour matches.
package editcomment

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrExitedWithoutSaving is returned when the user exits the editor without saving changes.
var ErrExitedWithoutSaving = errors.New("exited without saving")

// EditCommentWithEditor writes currentComment to a temp file, runs editor on it, and returns trimmed content if changed.
func EditCommentWithEditor(currentComment, editor string) (string, error) {
	tmp, err := os.CreateTemp("", "sshush-comment-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	defer tmp.Close()

	if _, err := tmp.WriteString(currentComment + "\n"); err != nil {
		return "", fmt.Errorf("write temp comment: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temp file: %w", err)
	}

	editorParts := strings.Fields(editor)
	if len(editorParts) == 0 {
		return "", fmt.Errorf("invalid editor command")
	}
	cmd := exec.Command(editorParts[0], append(editorParts[1:], tmpPath)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor failed: %w", err)
	}

	edited, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("read edited comment: %w", err)
	}
	trimmed := strings.TrimSpace(string(edited))
	if trimmed == strings.TrimSpace(currentComment) {
		return "", ErrExitedWithoutSaving
	}
	return trimmed, nil
}
