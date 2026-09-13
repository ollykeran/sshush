package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/ollykeran/sshush/internal/theme"
)

func newExportTestScreen() *ExportScreen {
	sk := NewSkeleton()
	sk.theme = theme.DefaultTheme()
	sk.styles = BuildStyles(sk.theme)
	return NewExportScreen(sk, "/tmp/agent.sock")
}

func TestExportFocusStartsOnLoadFile(t *testing.T) {
	s := newExportTestScreen()
	if s.ring.Index() != exportFocusLoadFile {
		t.Fatalf("focus=%d, want load file", s.ring.Index())
	}
}

func TestExportFocusDownToLoadAgent(t *testing.T) {
	s := newExportTestScreen()
	_, _ = s.Update(tea.KeyPressMsg{Code: 'j'})
	if s.ring.Index() != exportFocusLoadAgent {
		t.Fatalf("focus=%d, want load agent", s.ring.Index())
	}
}

func TestExportFocusSkipsPubActionsUntilLoaded(t *testing.T) {
	s := newExportTestScreen()
	for i := 0; i < 8; i++ {
		_, _ = s.Update(tea.KeyPressMsg{Code: 'j'})
	}
	idx := s.ring.Index()
	if idx == exportFocusCopy || idx == exportFocusSaveFile || idx == exportFocusPubKey {
		t.Fatalf("focus=%d should not reach pub/actions without key", idx)
	}
}

func TestExportFocusReachesCopyAfterPubKey(t *testing.T) {
	s := newExportTestScreen()
	s.pubKeyStr = "ssh-ed25519 AAAA test"
	s.syncRingSkip()
	s.ring.SetIndex(exportFocusLoadFile)
	_, _ = s.Update(tea.KeyPressMsg{Code: 'j'})
	_, _ = s.Update(tea.KeyPressMsg{Code: 'j'})
	if s.ring.Index() != exportFocusPubKey {
		t.Fatalf("focus=%d, want pub key", s.ring.Index())
	}
	_, _ = s.Update(tea.KeyPressMsg{Code: 'j'})
	if s.ring.Index() != exportFocusCopy {
		t.Fatalf("focus=%d, want copy", s.ring.Index())
	}
}
