package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/ollykeran/sshush/internal/utils"
)

func TestSearchedDirsText(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	ssh := filepath.Join(home, ".ssh")
	gone := filepath.Join(home, "gone")
	got := searchedDirsText([]string{gone, ssh, ssh}, []string{gone})
	want := utils.DisplayPath(gone) + " (unreadable), " + utils.DisplayPath(ssh)
	if got != want {
		t.Fatalf("searchedDirsText = %q, want %q", got, want)
	}
	if !strings.HasPrefix(utils.DisplayPath(ssh), "~") {
		t.Fatalf("home should show as ~, got %q", utils.DisplayPath(ssh))
	}
	if got := searchedDirsText(nil, nil); got != "" {
		t.Fatalf("no dirs: %q", got)
	}
}

// The Found Keys title names the searched directories, shortened with … to fit,
// and always keeps the label and the re-scan control.
func TestFoundTitleLineFitsWidth(t *testing.T) {
	_, agent := newAgentTestSkeleton()
	agent.searchedDirs = []string{"/srv/keys/team-a", "/srv/keys/team-b", "/srv/keys/team-c", "/home/someone/.ssh"}
	// Paths show as DisplayPath has them (e.g. D:\srv\keys\team-a on Windows).
	shown := make([]string, len(agent.searchedDirs))
	for i, d := range agent.searchedDirs {
		shown[i] = utils.DisplayPath(d)
	}

	wide := ansi.Strip(agent.foundTitleLine(" Found Keys", "from", 200))
	if !strings.Contains(wide, "from "+strings.Join(shown, ", ")) {
		t.Fatalf("wide title should list every dir: %q", wide)
	}

	narrow := ansi.Strip(agent.foundTitleLine(" Found Keys", "from", 60))
	if w := ansi.StringWidth(narrow); w > 60 {
		t.Fatalf("title is %d columns, over 60: %q", w, narrow)
	}
	if !strings.HasPrefix(narrow, " Found Keys from "+shown[0]) || !strings.Contains(narrow, "…") ||
		!strings.HasSuffix(narrow, "[f] search again") {
		t.Fatalf("narrow title should shorten the dirs only: %q", narrow)
	}

	tiny := ansi.Strip(agent.foundTitleLine(" Found Keys", "from", 30))
	if tiny != " Found Keys [f] search again" {
		t.Fatalf("with no room the dirs are left out: %q", tiny)
	}
}

// The search reports the directories it looked in, and the Agent page shows
// them beside Found Keys, or beside "No other keys found" when there are none.
func TestAgentShowsSearchedDirs(t *testing.T) {
	msg := discoverKeysCmd("", false)().(foundKeysMsg)
	wd, _ := os.Getwd()
	if !slices.Contains(msg.searched, wd) {
		t.Fatalf("searched %v does not include the current directory %s", msg.searched, wd)
	}

	_, agent := newAgentTestSkeleton()
	agent.width, agent.height = 160, 30
	_, _ = agent.Update(foundKeysMsg{searched: []string{"/srv/keys"}})
	keys := utils.DisplayPath("/srv/keys")
	if view := agentPlainView(agent); !strings.Contains(view, "No other keys found in "+keys) {
		t.Fatalf("empty list should say where it looked:\n%s", view)
	}

	_, _ = agent.Update(foundKeysMsg{
		paths:    []utils.KeyPath{{Path: "/srv/keys/id_test"}},
		searched: []string{"/srv/keys"},
	})
	if view := agentPlainView(agent); !strings.Contains(view, "Found Keys from "+keys) {
		t.Fatalf("Found Keys title should say where it looked:\n%s", view)
	}
}
