package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// newCreateSaveScreen returns a Create screen saving to name in a temp dir,
// focused on Save, and the full path it will write.
func newCreateSaveScreen(t *testing.T, name string) (*CreateScreen, string) {
	t.Helper()
	s := newCreateTestScreen()
	s.width, s.height = 120, 40
	dir := t.TempDir()
	s.dirInput.SetValue(dir)
	s.filenameIn.SetValue(name)
	s.ring.SetIndex(createFocusSave)
	s.updateButtonFocus()
	return s, filepath.Join(dir, name)
}

// runGenerate runs the commands Save returned and hands the key generation
// result back to the screen.
func runGenerate(t *testing.T, s *CreateScreen, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("Save returned no command; nothing was generated")
	}
	var msgs []tea.Msg
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				msgs = append(msgs, c())
			}
		}
	}
	for _, m := range msgs {
		if done, ok := m.(keyGenDoneMsg); ok {
			_, _ = s.Update(done)
			return
		}
	}
	t.Fatalf("no keyGenDoneMsg among %v", msgs)
}

func TestCreateSaveNewFileDoesNotAsk(t *testing.T) {
	s, path := newCreateSaveScreen(t, "id_new")
	_, cmd := s.Update(key("enter"))
	if s.HasModal() {
		t.Fatal("no file exists yet, so Save should not ask")
	}
	runGenerate(t, s, cmd)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("key not written: %v", err)
	}
}

func TestCreateSaveOverExistingFileAsksFirst(t *testing.T) {
	s, path := newCreateSaveScreen(t, "id_existing")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, cmd := s.Update(key("enter"))
	if cmd != nil || !s.HasModal() {
		t.Fatalf("Save over an existing file should ask first (cmd=%v, modal=%v)", cmd != nil, s.HasModal())
	}
	view := ansi.Strip(s.View().Content)
	// A long path wraps inside the box, so compare without borders and spacing.
	flat := strings.NewReplacer("│", "", " ", "", "\n", "").Replace(view)
	if !strings.Contains(view, "Overwrite key?") || !strings.Contains(flat, path+"alreadyexists") {
		t.Fatalf("popup should name the file:\n%s", view)
	}

	// No: nothing is written.
	_, cmd = s.Update(key("n"))
	if cmd != nil || s.HasModal() {
		t.Fatalf("answering no should close without saving (cmd=%v, modal=%v)", cmd != nil, s.HasModal())
	}
	if b, _ := os.ReadFile(path); string(b) != "old" {
		t.Fatal("file changed after answering no")
	}

	// Yes: the key replaces the file.
	_, _ = s.Update(key("enter"))
	_, cmd = s.Update(key("y"))
	runGenerate(t, s, cmd)
	if b, _ := os.ReadFile(path); string(b) == "old" {
		t.Fatal("file not overwritten after answering yes")
	}
	if s.status != "key saved" {
		t.Fatalf("status=%q, want key saved", s.status)
	}
}

// While the popup is open, keys reach it rather than the tab bar or the
// global hotkeys.
func TestCreateConfirmTakesKeysThroughSkeleton(t *testing.T) {
	s, path := newCreateSaveScreen(t, "id_existing")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	sk := s.sk
	sk.AddPage("create", "Create", s)
	sk.activeTab = 0
	sk.navFocus = navFocusScreen

	_, _ = sk.Update(key("enter"))
	if !s.HasModal() {
		t.Fatal("popup not open after Save")
	}
	_, _ = sk.Update(key("esc"))
	if s.HasModal() {
		t.Fatal("esc through the skeleton should close the popup")
	}
	if sk.activeTab != 0 || sk.navFocus != navFocusScreen {
		t.Fatalf("closing the popup moved focus: tab=%d navFocus=%d", sk.activeTab, sk.navFocus)
	}
}

func TestCreateFileExistsWarningSitsBelowSave(t *testing.T) {
	s, path := newCreateSaveScreen(t, "id_existing")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	view := ansi.Strip(s.View().Content)
	save := strings.Index(view, "Save")
	warn := strings.Index(view, "File exists")
	if save < 0 || warn < 0 || warn < save {
		t.Fatalf("warning should come after the Save button (save=%d warn=%d):\n%s", save, warn, view)
	}
}

// The result boxes are drawn before a key is generated, previewing the paths
// Save will write, so generating one does not shift the layout.
func TestCreateResultBoxesShownBeforeGenerating(t *testing.T) {
	s, path := newCreateSaveScreen(t, "id_new")
	before := ansi.Strip(s.View().Content)
	// Long temp paths wrap inside the boxes, so look for the file names.
	for _, want := range []string{"Public Key", "Private Key", "Public Key File",
		"shown here after Save", filepath.Base(path), filepath.Base(path) + ".pub"} {
		if !strings.Contains(before, want) {
			t.Fatalf("before generating, missing %q:\n%s", want, before)
		}
	}

	_, cmd := s.Update(key("enter"))
	runGenerate(t, s, cmd)
	after := ansi.Strip(s.View().Content)
	if strings.Contains(after, "shown here after Save") {
		t.Fatalf("placeholder still shown after generating:\n%s", after)
	}
	if !strings.Contains(after, "ssh-ed25519 ") {
		t.Fatalf("public key not shown after generating:\n%s", after)
	}
}
