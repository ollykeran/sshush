package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/theme"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func TestConfirmDialogKeys(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		want confirmResult
	}{
		{"y answers yes", []string{"y"}, confirmYes},
		{"n answers no", []string{"n"}, confirmNo},
		{"esc cancels", []string{"esc"}, confirmNo},
		{"enter on the default picks no", []string{"enter"}, confirmNo},
		{"left then enter picks yes", []string{"left", "enter"}, confirmYes},
		{"tab moves between the buttons", []string{"tab", "tab", "enter"}, confirmNo},
		{"other keys leave it open", []string{"q", "x"}, confirmPending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newConfirmDialog()
			c.Show("Overwrite?", "it exists")
			got := confirmPending
			for _, k := range tt.keys {
				got = c.Update(key(k))
			}
			if got != tt.want {
				t.Fatalf("result=%d, want %d", got, tt.want)
			}
			if c.active != (tt.want == confirmPending) {
				t.Fatalf("active=%v after result %d", c.active, got)
			}
		})
	}
}

// Showing the dialog again selects No again, even after Yes was selected.
func TestConfirmDialogReopensOnNo(t *testing.T) {
	c := newConfirmDialog()
	c.Show("Overwrite?", "it exists")
	c.Update(key("left"))
	c.Update(key("esc"))
	c.Show("Overwrite?", "it exists")
	if got := c.Update(key("enter")); got != confirmNo {
		t.Fatalf("enter after reopening = %d, want no", got)
	}
}

func TestConfirmDialogClick(t *testing.T) {
	st := BuildStyles(theme.DefaultTheme())
	for _, tt := range []struct {
		button string
		want   confirmResult
	}{{"Yes", confirmYes}, {"No", confirmNo}} {
		t.Run(tt.button, func(t *testing.T) {
			c := newConfirmDialog()
			c.Show("Overwrite?", "it exists")
			zone.Scan(c.View(st, 60))
			z := waitForZone(c.buttons.ZonePrefix + tt.button)
			if z == nil {
				t.Fatalf("zone for %s not registered", tt.button)
			}
			if got := c.HandleMouse(z.StartX, z.StartY); got != tt.want {
				t.Fatalf("click on %s = %d, want %d", tt.button, got, tt.want)
			}
		})
	}
}
