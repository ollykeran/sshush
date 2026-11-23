package clipboard

import (
	"errors"
	"testing"

	"github.com/atotto/clipboard"
)

var procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")

// The text has to arrive like any other copy, and the marks that keep it out
// of clipboard history and cloud sync have to be on the clipboard beside it.
func TestCopySecret_setsTheTextAndTheExclusionFormats(t *testing.T) {
	// Not ASCII only: the text goes on as UTF-16 and must come back whole.
	const secret = "abandon ability able about — naïve café"
	if err := CopySecret(secret); err != nil {
		if errors.Is(err, errOpenClipboard) {
			// A runner with no interactive session has no clipboard to open.
			t.Skipf("no clipboard here: %v", err)
		}
		t.Fatal(err)
	}
	// Whatever happens below, the next thing copied on this machine should
	// not have to push a test phrase out of the way.
	t.Cleanup(func() { _ = clipboard.WriteAll("") })

	got, err := clipboard.ReadAll()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != secret {
		t.Fatalf("read back %q, want %q", got, secret)
	}

	for _, name := range []string{formatExcludeFromMonitors, formatCanIncludeInHistory, formatCanUploadToCloud} {
		format, err := registerFormat(name)
		if err != nil {
			t.Fatal(err)
		}
		if r, _, _ := procIsClipboardFormatAvailable.Call(format); r == 0 {
			t.Errorf("format %s is not on the clipboard", name)
		}
	}
}

// Copy is for public keys and leaves the clipboard an ordinary one, history
// and all: only CopySecret opts out.
func TestCopy_leavesTheExclusionFormatsOff(t *testing.T) {
	if err := CopySecret("probe"); err != nil {
		if errors.Is(err, errOpenClipboard) {
			t.Skipf("no clipboard here: %v", err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clipboard.WriteAll("") })
	if err := Copy("ssh-ed25519 AAAA test"); err != nil {
		t.Fatal(err)
	}
	format, err := registerFormat(formatExcludeFromMonitors)
	if err != nil {
		t.Fatal(err)
	}
	if r, _, _ := procIsClipboardFormatAvailable.Call(format); r != 0 {
		t.Error("an ordinary copy carries the exclusion format")
	}
}
