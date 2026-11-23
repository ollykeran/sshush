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
var ErrExitedWithoutSaving = errors.New("editcomment: exited without saving")

// Validate rejects comments containing embedded newlines, since comments are written
// as a single line into PEM key headers and .pub/authorized_keys-style files.
func Validate(comment string) error {
	if strings.ContainsAny(comment, "\r\n") {
		return errors.New("comment cannot contain newlines")
	}
	return nil
}

// EditCommentWithEditor writes currentComment to a temp file, runs editor on it, and returns trimmed content if changed.
func EditCommentWithEditor(currentComment, editor string) (string, error) {
	tmp, err := os.CreateTemp("", "sshush-comment-*")
	if err != nil {
		return "", fmt.Errorf("editcomment: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	defer tmp.Close()

	if _, err := tmp.WriteString(currentComment + "\n"); err != nil {
		return "", fmt.Errorf("editcomment: write temp comment: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("editcomment: close temp file: %w", err)
	}

	program, args, err := editorCommand(editor)
	if err != nil {
		return "", err
	}
	cmd := exec.Command(program, append(args, tmpPath)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editcomment: editor failed: %w", err)
	}

	edited, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("editcomment: read edited comment: %w", err)
	}
	trimmed := strings.TrimSpace(string(edited))
	if trimmed == strings.TrimSpace(currentComment) {
		return "", ErrExitedWithoutSaving
	}
	if err := Validate(trimmed); err != nil {
		return "", err
	}
	return trimmed, nil
}

// editorCommand turns an editor setting ($EDITOR, --editor) into a program and
// its arguments. A setting that names a program as it stands is taken whole, so
// a path with spaces in it works unquoted; otherwise it is split on spaces,
// with single or double quotes keeping a part together:
//
//	code --wait
//	"C:\Program Files\Notepad++\notepad++.exe" -multiInst
//
// A backslash is an ordinary character, never an escape, or no Windows path
// would survive.
func editorCommand(editor string) (program string, args []string, err error) {
	editor = strings.TrimSpace(editor)
	if editor == "" {
		return "", nil, fmt.Errorf("editcomment: invalid editor command")
	}
	if _, err := exec.LookPath(editor); err == nil {
		return editor, nil, nil
	}

	var parts []string
	var part strings.Builder
	inPart := false
	var quote rune
	for _, r := range editor {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				part.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
			inPart = true
		case r == ' ' || r == '\t':
			if inPart {
				parts = append(parts, part.String())
				part.Reset()
				inPart = false
			}
		default:
			part.WriteRune(r)
			inPart = true
		}
	}
	if quote != 0 {
		return "", nil, fmt.Errorf("editcomment: unterminated quote in editor command %q", editor)
	}
	if inPart {
		parts = append(parts, part.String())
	}
	if len(parts) == 0 {
		return "", nil, fmt.Errorf("editcomment: invalid editor command")
	}
	return parts[0], parts[1:], nil
}
