package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/editcomment"
	"github.com/ollykeran/sshush/internal/keys"
	"github.com/ollykeran/sshush/internal/vault"
)

// commentOverlaySavedMsg reports the result of saving a comment edited via commentOverlay.
type commentOverlaySavedMsg struct {
	fingerprint string
	comment     string
	err         error
}

// commentOverlay is a small modal for editing an agent-loaded key's comment in place
// (issue #34: press 'e' on a selected key in the agent screen).
type commentOverlay struct {
	active          bool
	fingerprint     string
	keyType         string
	originalComment string
	commentIn       textinput.Model
	status          string
	statusErr       bool
}

func newCommentOverlay() commentOverlay {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "key comment"
	return commentOverlay{commentIn: ti}
}

// Show opens the overlay pre-seeded with the selected key's current comment.
func (c *commentOverlay) Show(fingerprint, keyType, comment string) tea.Cmd {
	c.active = true
	c.fingerprint = fingerprint
	c.keyType = keyType
	c.originalComment = comment
	c.status = ""
	c.statusErr = false
	c.commentIn.SetValue(comment)
	c.commentIn.CursorEnd()
	return c.commentIn.Focus()
}

func (c *commentOverlay) Hide() {
	c.active = false
	c.commentIn.Blur()
}

// Update handles key input while the overlay is active. Returns the command to run
// (e.g. the save command) and whether the caller should keep the overlay open.
func (c *commentOverlay) Update(msg tea.KeyPressMsg, socketPath string) tea.Cmd {
	switch msg.String() {
	case "esc":
		c.Hide()
		return nil
	case "enter":
		comment := strings.TrimSpace(c.commentIn.Value())
		if comment == strings.TrimSpace(c.originalComment) {
			c.status = "no changes"
			c.statusErr = false
			return nil
		}
		if comment == "" {
			c.status = "comment cannot be empty"
			c.statusErr = true
			return nil
		}
		if err := editcomment.Validate(comment); err != nil {
			c.status = err.Error()
			c.statusErr = true
			return nil
		}
		return saveCommentOverlayCmd(socketPath, c.fingerprint, comment)
	}
	var cmd tea.Cmd
	c.commentIn, cmd = c.commentIn.Update(msg)
	return cmd
}

func (c *commentOverlay) View(st Styles, width int) string {
	title := st.SectionTitleStyle.Render(fmt.Sprintf(" Edit comment (%s)", c.keyType))
	box := st.SectionBox("Comment", c.commentIn.View(), width, true)
	sections := []string{title, box}
	if c.status != "" {
		statusStyle := st.FocusStyle
		if c.statusErr {
			statusStyle = st.ErrorStyle
		}
		sections = append(sections, statusStyle.Render("  "+c.status))
	}
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// saveCommentOverlayCmd resolves the source file for fingerprint, writes the new
// comment to disk (and .pub if present), persists it to the vault when the running
// agent is vault-backed, and reloads the key in the agent.
func saveCommentOverlayCmd(socketPath, fingerprint, comment string) tea.Cmd {
	return func() tea.Msg {
		path := agent.GetFilepath(fingerprint)
		if path == "" {
			return commentOverlaySavedMsg{fingerprint: fingerprint, err: fmt.Errorf("source file path unknown for this key; edit via CLI with --filepath")}
		}

		_, rawKey, _, err := keys.LoadKeyMaterial(path)
		if err != nil {
			return commentOverlaySavedMsg{fingerprint: fingerprint, err: err}
		}
		if err := keys.SaveWithComment(rawKey, comment, path); err != nil {
			return commentOverlaySavedMsg{fingerprint: fingerprint, err: err}
		}

		session, err := agent.Open(socketPath)
		if err != nil {
			return commentOverlaySavedMsg{fingerprint: fingerprint, comment: comment, err: fmt.Errorf("key file updated on disk but agent reload failed: %w", err)}
		}
		defer session.Close()

		backend, backendErr := session.Backend()
		if backendErr == nil && backend.Mode == "vault" {
			if err := vault.SetComment(session, fingerprint, comment); err != nil {
				return commentOverlaySavedMsg{fingerprint: fingerprint, comment: comment, err: fmt.Errorf("key file updated on disk but vault comment not updated: %w", err)}
			}
			if err := vault.AddPrivateKeyFile(session, path, true); err != nil {
				return commentOverlaySavedMsg{fingerprint: fingerprint, comment: comment, err: fmt.Errorf("key file updated on disk but agent reload failed: %w", err)}
			}
			return commentOverlaySavedMsg{fingerprint: fingerprint, comment: comment}
		}

		if _, err := session.RemoveByFingerprint(fingerprint); err != nil {
			return commentOverlaySavedMsg{fingerprint: fingerprint, comment: comment, err: fmt.Errorf("key file updated on disk but agent reload failed: %w", err)}
		}
		if err := session.AddKeyFromPath(path); err != nil {
			return commentOverlaySavedMsg{fingerprint: fingerprint, comment: comment, err: fmt.Errorf("key file updated on disk but agent reload failed: %w", err)}
		}
		return commentOverlaySavedMsg{fingerprint: fingerprint, comment: comment}
	}
}
