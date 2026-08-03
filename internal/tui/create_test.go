package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/ollykeran/sshush/internal/theme"
)

func newCreateTestScreen() *CreateScreen {
	sk := NewSkeleton()
	sk.theme = theme.DefaultTheme()
	sk.styles = BuildStyles(sk.theme)
	return NewCreateScreen(sk)
}

func TestCreateFocusDownSkipsOptionsForEd25519(t *testing.T) {
	s := newCreateTestScreen()
	if s.ring.Index() != createFocusType {
		t.Fatalf("start focus=%d, want type", s.ring.Index())
	}
	_, _ = s.Update(tea.KeyPressMsg{Code: 'j'})
	if s.ring.Index() != createFocusComment {
		t.Fatalf("after down focus=%d, want comment (skip options)", s.ring.Index())
	}
}

func TestCreateFocusIncludesOptionsForRSA(t *testing.T) {
	s := newCreateTestScreen()
	s.typeRow.Active = 1 // rsa
	s.syncKeyTypeChange()
	s.ring.SetIndex(createFocusType)

	_, _ = s.Update(tea.KeyPressMsg{Code: 'j'})
	if s.ring.Index() != createFocusOptions {
		t.Fatalf("after down on rsa focus=%d, want options", s.ring.Index())
	}
}

func TestCreateFocusPrevFromTypeGoesToTabBar(t *testing.T) {
	s := newCreateTestScreen()
	_, cmd := s.Update(tea.KeyPressMsg{Code: 'k'})
	if cmd == nil {
		t.Fatal("expected navToTabBar cmd from focus prev at top")
	}
	if m := cmd(); m == nil {
		t.Fatal("expected NavToTabBarMsg")
	} else if _, ok := m.(NavToTabBarMsg); !ok {
		t.Fatalf("got %T, want NavToTabBarMsg", m)
	}
}

func TestCreateLeftRightChangesKeyType(t *testing.T) {
	s := newCreateTestScreen()
	if s.currentKeyType() != "ed25519" {
		t.Fatalf("start type=%s", s.currentKeyType())
	}
	_, _ = s.Update(tea.KeyPressMsg{Code: 'l'})
	if s.currentKeyType() != "rsa" {
		t.Fatalf("after right type=%s, want rsa", s.currentKeyType())
	}
}
