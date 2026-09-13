package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/ollykeran/sshush/internal/theme"
)

func TestExportFocusRingSkipsPubkeyUntilLoaded(t *testing.T) {
	sk := NewSkeleton()
	sk.theme = theme.DefaultTheme()
	sk.styles = BuildStyles(sk.theme)
	s := NewExportScreen(sk, "/tmp/agent.sock")

	updated, _ := s.Update(tea.KeyPressMsg{Code: 'j'})
	s = updated.(*ExportScreen)
	if s.ring.Index() != exportFocusLoadAgent {
		t.Fatalf("focus=%d, want load agent", s.ring.Index())
	}
	updated, _ = s.Update(tea.KeyPressMsg{Code: 'j'})
	s = updated.(*ExportScreen)
	// no pubkey → should clamp on load agent (not jump into copy/save)
	if s.ring.Index() != exportFocusLoadAgent {
		t.Fatalf("focus=%d, want stay on load agent without pubkey", s.ring.Index())
	}
}

func TestExportLoadsPubKeyAdvancesFocus(t *testing.T) {
	sk := NewSkeleton()
	sk.theme = theme.DefaultTheme()
	sk.styles = BuildStyles(sk.theme)
	s := NewExportScreen(sk, "/tmp/agent.sock")

	updated, _ := s.Update(exportKeyLoadedMsg{
		pubKeyStr: "ssh-ed25519 AAAAC3 comment",
		keyType:   "ssh-ed25519",
	})
	s = updated.(*ExportScreen)
	if s.ring.Index() != exportFocusPubKey {
		t.Fatalf("focus=%d, want pubkey", s.ring.Index())
	}
	updated, _ = s.Update(tea.KeyPressMsg{Code: 'j'})
	s = updated.(*ExportScreen)
	if s.ring.Index() != exportFocusCopy {
		t.Fatalf("focus=%d, want copy", s.ring.Index())
	}
}

func TestEditFocusRingToTabs(t *testing.T) {
	sk := NewSkeleton()
	sk.theme = theme.DefaultTheme()
	sk.styles = BuildStyles(sk.theme)
	s := NewEditScreen(sk, "/tmp/agent.sock")

	_, cmd := s.Update(tea.KeyPressMsg{Code: 'k'})
	if cmd == nil {
		t.Fatal("expected navToTabBar")
	}
	if _, ok := cmd().(NavToTabBarMsg); !ok {
		t.Fatalf("got %T", cmd())
	}
}
