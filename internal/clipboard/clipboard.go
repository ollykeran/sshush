// Package clipboard copies text to the system clipboard, for the CLI's and the
// TUI's "copy public key" and for the vault's recovery phrase.
package clipboard

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CopyCmdEnv names the environment variable that replaces the platform's way
// of copying with a command of the user's choosing, which is given the text on
// its standard input: `xsel --clipboard --input`, `tmux load-buffer -`,
// `clip.exe` from inside WSL. Empty, or only whitespace, is the same as unset.
const CopyCmdEnv = "SSHUSH_COPY_CMD"

// Copy writes text to the system clipboard: wl-copy under Wayland and xclip
// under X11 on Linux, pbcopy on macOS, the clipboard API on Windows, or on any
// platform the command in $SSHUSH_COPY_CMD when that is set. It returns an
// error if the platform is unsupported or the copy fails.
func Copy(text string) error {
	argv, err := overrideCommand()
	if err != nil {
		return err
	}
	if argv != nil {
		return runCommand(argv, text)
	}
	return copyText(text)
}

// CopySecret is Copy for text that should not outlive the paste it was copied
// for: a recovery phrase, not a public key. On Windows the clipboard is not
// only a clipboard: Win+V keeps a history of what passed through it, and cloud
// clipboard sends it to the user's other devices. CopySecret marks the text so
// that Windows does neither. The other platforms have no such history of their
// own to opt out of, so there it is the same as Copy.
//
// $SSHUSH_COPY_CMD is honoured here as well, since whoever set it did so
// because the default does not reach the clipboard they paste from; but the
// marks are the clipboard API's to set, so with it set, keeping the text out
// of any history is up to the command.
func CopySecret(text string) error {
	argv, err := overrideCommand()
	if err != nil {
		return err
	}
	if argv != nil {
		return runCommand(argv, text)
	}
	return copySecret(text)
}

// overrideCommand returns the command in $SSHUSH_COPY_CMD as a program and its
// arguments, or nil when the variable is unset or blank.
func overrideCommand() ([]string, error) {
	value := os.Getenv(CopyCmdEnv)
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	argv, err := splitCommand(value)
	if err != nil {
		return nil, fmt.Errorf("clipboard: %s: %w", CopyCmdEnv, err)
	}
	return argv, nil
}

// splitCommand turns a command line into a program and its arguments, without
// a shell: no variables, globs, pipes or redirections, so nothing in the value
// is ever interpreted beyond where one argument ends and the next begins. A
// value that names a program as it stands is taken whole, so a path with
// spaces in it works unquoted; otherwise it is split on spaces and tabs, with
// single or double quotes keeping a part together:
//
//	xsel --clipboard --input
//	"C:\Program Files\Some Tool\copy.exe" --utf8
//
// A backslash is an ordinary character, never an escape, or no Windows path
// would survive. These are the rules `sshush edit` applies to $EDITOR.
func splitCommand(command string) ([]string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("empty command")
	}
	if _, err := exec.LookPath(command); err == nil {
		return []string{command}, nil
	}

	var parts []string
	var part strings.Builder
	inPart := false
	var quote rune
	for _, r := range command {
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
		return nil, fmt.Errorf("unterminated quote in %q", command)
	}
	if inPart {
		parts = append(parts, part.String())
	}
	if len(parts) == 0 || parts[0] == "" {
		return nil, fmt.Errorf("no program in %q", command)
	}
	return parts, nil
}

// runCommand runs argv with text on its standard input, and names the program
// in the error if it cannot be started or fails.
//
// Its stdout and stderr are left unattached, not captured: wl-copy and xclip
// fork a child that stays behind to serve the clipboard, and that child would
// hold a captured stream open, so that Run never returned.
func runCommand(argv []string, text string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clipboard: %s: %w", argv[0], err)
	}
	return nil
}
