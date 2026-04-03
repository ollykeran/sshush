package style

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestBox_frame_matches_natural_width(t *testing.T) {
	rebuildStyles()
	content := "hello"
	nat := box.Render(content)
	line := strings.Split(nat, "\n")[0]
	got := lipgloss.Width(line)
	inner := lipgloss.Width(content)
	want := inner + box.GetHorizontalFrameSize()
	if got != want {
		t.Fatalf("natural outer width %d, want inner+frame=%d+%d=%d", got, inner, box.GetHorizontalFrameSize(), want)
	}
}

func TestRenderBox_hugs_content_when_under_limit(t *testing.T) {
	rebuildStyles()
	s := "a\nb"
	wideLimit := renderBox(s, 500)
	natural := box.Render(s)
	if wideLimit != natural {
		t.Fatalf("expected natural box when under limit")
	}
}

func TestRenderBox_wraps_when_over_limit(t *testing.T) {
	rebuildStyles()
	long := strings.Repeat("x", 30) + " " + strings.Repeat("y", 30)
	out := renderBox(long, 40)
	maxW := 0
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > maxW {
			maxW = w
		}
	}
	if maxW > 50 {
		t.Fatalf("expected wrapped output, max visual line width %d", maxW)
	}
}
