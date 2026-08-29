package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/ollykeran/sshush/internal/theme"
)

func newEditTestScreen() *EditScreen {
	sk := NewSkeleton()
	sk.theme = theme.DefaultTheme()
	sk.styles = BuildStyles(sk.theme)
	return NewEditScreen(sk, "/tmp/agent.sock")
}

func TestEditFocusStartsOnSelectFile(t *testing.T) {
	s := newEditTestScreen()
	if s.ring.Index() != editFocusSelectFile {
		t.Fatalf("focus=%d, want select file", s.ring.Index())
	}
}

func TestEditFocusPrevFromSelectGoesToTabBar(t *testing.T) {
	s := newEditTestScreen()
	_, cmd := s.Update(tea.KeyPressMsg{Code: 'k'})
	if cmd == nil {
		t.Fatal("expected navToTabBar cmd")
	}
	if m := cmd(); m == nil {
		t.Fatal("expected NavToTabBarMsg")
	} else if _, ok := m.(NavToTabBarMsg); !ok {
		t.Fatalf("got %T, want NavToTabBarMsg", m)
	}
}

func TestEditFocusAdvancesToCommentWhenKeyLoaded(t *testing.T) {
	s := newEditTestScreen()
	s.rawKey = struct{}{}
	s.loadedPath = "/tmp/id_ed25519"
	s.ring.SetSkip(func(i int) bool {
		return s.rawKey == nil && i > editFocusSelectFile
	})
	s.ring.SetIndex(editFocusSelectFile)
	_, _ = s.Update(tea.KeyPressMsg{Code: 'j'})
	if s.ring.Index() != editFocusComment {
		t.Fatalf("focus=%d, want comment", s.ring.Index())
	}
}
