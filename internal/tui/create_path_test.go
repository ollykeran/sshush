package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone"
)

func TestSplitKeyPath(t *testing.T) {
	sep := string(filepath.Separator)
	tests := []struct{ in, dir, file string }{
		{"~/.ssh/id_ed25519", "~/.ssh/", "id_ed25519"},
		{"id_rsa", "", "id_rsa"},
		{"/srv/keys/", "/srv/keys/", ""},
		{"", "", ""},
		{"keys" + sep + "work", "keys" + sep, "work"},
	}
	for _, tt := range tests {
		dir, file := splitKeyPath(tt.in)
		if dir != tt.dir || file != tt.file {
			t.Errorf("splitKeyPath(%q) = %q, %q; want %q, %q", tt.in, dir, file, tt.dir, tt.file)
		}
	}
}

// The Path field starts at ~/.ssh/id_ed25519, shown with ~ for home.
func TestCreatePathDefault(t *testing.T) {
	s := newCreateTestScreen()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got, want := s.pathIn.Value(), "~"+string(filepath.Separator)+filepath.Join(".ssh", "id_ed25519"); got != want {
		t.Fatalf("default Path = %q, want %q", got, want)
	}
	if _, _, full := s.savePath(); full != filepath.Join(home, ".ssh", "id_ed25519") {
		t.Fatalf("default save path = %q", full)
	}
}

// Changing the key type renames a default file name (id_ed25519 -> id_rsa),
// keeping whatever directory the Path has, but leaves a typed file name alone.
func TestCreatePathFollowsKeyType(t *testing.T) {
	s := newCreateTestScreen()
	s.ring.SetIndex(createFocusType)
	s.pathIn.SetValue("~/work/keys/id_ed25519")
	_, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // ed25519 -> rsa
	if got := s.pathIn.Value(); got != "~/work/keys/id_rsa" {
		t.Fatalf("after switching to rsa, Path = %q, want ~/work/keys/id_rsa", got)
	}
	_, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // rsa -> ecdsa
	if got := s.pathIn.Value(); got != "~/work/keys/id_ecdsa" {
		t.Fatalf("after switching to ecdsa, Path = %q", got)
	}

	s.pathIn.SetValue("~/work/keys/deploy")
	_, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyLeft}) // ecdsa -> rsa
	if got := s.pathIn.Value(); got != "~/work/keys/deploy" {
		t.Fatalf("a typed file name should stay: Path = %q", got)
	}
}

func TestCreateSavePathResolution(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	tmp := t.TempDir()
	sep := string(filepath.Separator)
	tests := []struct {
		name, value, want string
	}{
		{"file path", filepath.Join(tmp, "deploy"), filepath.Join(tmp, "deploy")},
		{"~ is expanded", "~" + sep + filepath.Join("keys", "work"), filepath.Join(home, "keys", "work")},
		{"existing directory gets the default name", tmp, filepath.Join(tmp, "id_ed25519")},
		{"trailing separator gets the default name", filepath.Join(tmp, "new") + sep, filepath.Join(tmp, "new", "id_ed25519")},
		{"empty means the default", "  ", filepath.Join(home, ".ssh", "id_ed25519")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newCreateTestScreen()
			s.pathIn.SetValue(tt.value)
			dir, file, full := s.savePath()
			if full != tt.want || dir != filepath.Dir(tt.want) || file != filepath.Base(tt.want) {
				t.Fatalf("savePath() = %q, %q, %q; want %q", dir, file, full, tt.want)
			}
		})
	}
}

// Down from Comment goes to Path, then Save: there is no separate Filename.
func TestCreateFocusCommentPathSave(t *testing.T) {
	s := newCreateTestScreen()
	s.ring.SetIndex(createFocusComment)
	s.updateButtonFocus()
	_, _ = s.Update(tea.KeyPressMsg{Code: 'j'})
	if s.ring.Index() != createFocusPath || !s.pathIn.Focused() {
		t.Fatalf("after comment: focus=%d pathFocused=%v, want the Path field", s.ring.Index(), s.pathIn.Focused())
	}
	_, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if s.ring.Index() != createFocusSave {
		t.Fatalf("after path: focus=%d, want Save", s.ring.Index())
	}
}

func TestCreateClickFocusesPath(t *testing.T) {
	s := newCreateTestScreen()
	s.width, s.height = 120, 40
	view := s.View().Content
	if strings.Contains(view, "Directory") || strings.Contains(view, "Filename") {
		t.Fatal("the Directory and Filename boxes should be gone")
	}
	zone.Scan(view)
	z := waitForZone(s.zonePrefix + "path")
	if z == nil {
		t.Fatal("Path zone not registered")
	}
	_, _ = s.Update(tea.MouseReleaseMsg{X: z.StartX + 3, Y: z.StartY + 2, Button: tea.MouseLeft})
	if s.ring.Index() != createFocusPath || !s.pathIn.Focused() {
		t.Fatalf("click: focus=%d pathFocused=%v, want the Path field", s.ring.Index(), s.pathIn.Focused())
	}
}
