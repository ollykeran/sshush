package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone"
)

// confirmResult is what a key press or click did to an open confirmDialog.
type confirmResult int

const (
	confirmPending confirmResult = iota // still open, waiting for an answer
	confirmYes                          // answered yes; the dialog has closed
	confirmNo                           // answered no or cancelled; the dialog has closed
)

const (
	confirmBtnYes = iota
	confirmBtnNo
)

// confirmDialog is a yes/no popup for a question that guards something hard to
// undo, such as overwriting a file. A page that owns one shows its View in
// place of the page while it is open, reports HasModal, and passes key presses
// and clicks to Update and HandleMouse, acting on the confirmResult.
//
// Keys: y answers yes; n or esc answers no; left/right or tab move between the
// buttons and enter picks the selected one. No is selected when it opens, so
// a stray enter does not confirm.
type confirmDialog struct {
	active  bool
	title   string
	message string
	buttons ButtonRow
}

func newConfirmDialog() confirmDialog {
	btns := NewButtonRow("Yes", "No")
	btns.ZonePrefix = zone.NewPrefix() + "confirm-"
	btns.Focused = true
	return confirmDialog{buttons: btns}
}

// Show opens the dialog with a title and a message, which may span lines.
func (c *confirmDialog) Show(title, message string) {
	c.active = true
	c.title = title
	c.message = message
	c.buttons.Active = confirmBtnNo
}

func (c *confirmDialog) close(result confirmResult) confirmResult {
	c.active = false
	return result
}

// Update handles a key press while the dialog is open.
func (c *confirmDialog) Update(msg tea.KeyPressMsg) confirmResult {
	switch msg.String() {
	case "y", "Y":
		return c.close(confirmYes)
	case "n", "N", "esc":
		return c.close(confirmNo)
	case "left", "h", "shift+tab":
		c.buttons.Left()
	case "right", "l", "tab":
		c.buttons.Right()
	case "enter", "space":
		if c.buttons.Active == confirmBtnYes {
			return c.close(confirmYes)
		}
		return c.close(confirmNo)
	}
	return confirmPending
}

// HandleMouse answers the dialog when a click lands on one of its buttons.
func (c *confirmDialog) HandleMouse(x, y int) confirmResult {
	switch c.buttons.HandleMouse(x, y) {
	case confirmBtnYes:
		return c.close(confirmYes)
	case confirmBtnNo:
		return c.close(confirmNo)
	}
	return confirmPending
}

// View renders the dialog, width columns wide, for the page to centre.
func (c *confirmDialog) View(st Styles, width int) string {
	title := st.SectionTitleStyle.Render(" " + c.title)
	box := st.WarnBorderStyle.Width(SectionInnerWidth(width)).Render(c.message)
	hint := st.DimStyle.Render("  y / n")
	buttons := lipgloss.JoinHorizontal(lipgloss.Center, " "+c.buttons.View(st), hint)
	return lipgloss.JoinVertical(lipgloss.Left, title, box, "", buttons)
}
