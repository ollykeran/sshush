package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/theme"
)

// newTestFileSelector creates a FileSelector rooted at dir, with its file
// list already loaded (readDir run synchronously).
func newTestFileSelector(t *testing.T, dir string) *FileSelector {
	t.Helper()
	st := BuildStyles(theme.DefaultTheme())
	fs := NewFileSelector(ModeLoadFile, "Select file", st)
	fs.picker.Model.CurrentDirectory = dir
	fs.SetHeight(20)

	initCmd := fs.Show()
	if initCmd == nil {
		t.Fatal("Show() returned nil init cmd")
	}
	fs.Update(initCmd())
	return fs
}

// renderAndScan renders the selector and registers its zone marks with the
// package-wide bubblezone manager. zone.Scan() feeds the manager's worker
// goroutine asynchronously, so this blocks until the "box" zone — whose end
// marker is the last one the scanner emits, since it wraps every row — is
// visible, guaranteeing every zone from this frame is present once it returns.
func renderAndScan(t *testing.T, fs *FileSelector, st Styles) {
	t.Helper()
	view := fs.View(100, 30, true, st)
	zone.Scan(view)
	mustZone(t, fs.zonePrefix+"box")
}

// mustZone polls for the zone's bounds, since zone.Scan() feeds the manager's
// worker goroutine asynchronously (see bubblezone's Manager.Scan doc comment).
func mustZone(t *testing.T, id string) *zone.ZoneInfo {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if z := zone.Get(id); z != nil {
			return z
		}
		if time.Now().After(deadline) {
			t.Fatalf("zone %q not found after render/scan", id)
		}
		time.Sleep(time.Millisecond)
	}
}

func click(fs *FileSelector, z *zone.ZoneInfo) tea.Cmd {
	return fs.Update(tea.MouseReleaseMsg{X: z.StartX, Y: z.StartY, Button: tea.MouseLeft})
}

func TestFileSelectorMouseClickSelectsFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "id_ed25519"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	fs := newTestFileSelector(t, dir)
	st := BuildStyles(theme.DefaultTheme())
	renderAndScan(t, fs, st)

	row := mustZone(t, fs.zonePrefix+"row-0")
	cmd := click(fs, row)
	if cmd == nil {
		t.Fatal("expected a cmd from clicking the file row")
	}

	var gotSelected bool
	var check func(tea.Msg)
	check = func(msg tea.Msg) {
		switch m := msg.(type) {
		case tea.BatchMsg:
			for _, c := range m {
				if c != nil {
					check(c())
				}
			}
		case FileSelectedMsg:
			gotSelected = true
			want := filepath.Join(dir, "id_ed25519")
			if m.Path != want {
				t.Errorf("selected path = %q, want %q", m.Path, want)
			}
		}
	}
	check(cmd())
	if !gotSelected {
		t.Fatal("expected FileSelectedMsg from clicking the file row")
	}
}

func TestFileSelectorMouseClickEntersDirectory(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}

	fs := newTestFileSelector(t, dir)
	st := BuildStyles(theme.DefaultTheme())
	renderAndScan(t, fs, st)

	row := mustZone(t, fs.zonePrefix+"row-0")
	click(fs, row)

	if got := fs.picker.CurrentDirectory(); got != sub {
		t.Fatalf("CurrentDirectory = %q, want %q", got, sub)
	}
}

func TestFileSelectorMouseClickUpdir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}

	fs := newTestFileSelector(t, sub)
	st := BuildStyles(theme.DefaultTheme())
	renderAndScan(t, fs, st)

	updir := mustZone(t, fs.zonePrefix+"updir")
	click(fs, updir)

	if got := fs.picker.CurrentDirectory(); got != dir {
		t.Fatalf("CurrentDirectory = %q, want %q", got, dir)
	}
}

func TestFileSelectorMouseClickOutsideCancels(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "id_ed25519"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	fs := newTestFileSelector(t, dir)
	st := BuildStyles(theme.DefaultTheme())
	renderAndScan(t, fs, st)

	box := mustZone(t, fs.zonePrefix+"box")
	cmd := fs.Update(tea.MouseReleaseMsg{X: box.StartX - 1, Y: box.StartY, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("expected a cmd from clicking outside the picker box")
	}
	if _, ok := cmd().(FilePickerCancelledMsg); !ok {
		t.Fatalf("expected FilePickerCancelledMsg, got %T", cmd())
	}
}
