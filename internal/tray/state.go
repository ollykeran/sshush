// Package tray is a Windows notification-area front end for sshushd. It is a
// client like the CLI and TUI: it reaches the agent over the same socket (a
// named pipe on Windows) through agent.Session and vaultops, never runs inside
// the daemon, and polls rather than being told about changes.
//
// Only the Win32 layer is Windows-specific; the mapping from what the agent
// reports to what the tray shows, and the poller, build everywhere so they can
// be tested on any OS.
package tray

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ollykeran/sshush/internal/agent"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// Icon names the tray icon for one agent state.
type Icon int

// The four states the tray can show.
const (
	IconUnreachable Icon = iota // no agent answered
	IconKeys                    // a plain keyring agent, nothing to lock
	IconLocked                  // a vault agent with its master key wiped
	IconUnlocked                // a vault agent holding its master key
)

// View is everything the tray renders for one poll. Two equal Views render
// identically, so the tray redraws only when the View changes. The menu shows
// only this; everything that changes the agent is in the main window.
type View struct {
	Icon    Icon
	Tooltip string
	// Heading names the agent and its state, such as "Vault agent, unlocked".
	Heading string
	// Keys holds one entry per key the agent serves, in agent order.
	Keys []KeyItem
	// Status, when not empty, stands in for Keys: why there is nothing to list.
	Status string
}

// KeyItem is one key in the menu.
type KeyItem struct {
	Label       string // "comment  SHA256:…"
	Fingerprint string // SHA256 fingerprint, empty when the key does not parse
}

// Equal reports whether v and o render identically.
func (v View) Equal(o View) bool {
	return v.Icon == o.Icon && v.Tooltip == o.Tooltip && v.Heading == o.Heading &&
		v.Status == o.Status && slices.Equal(v.Keys, o.Keys)
}

// Snapshot is what one poll learned from the agent. Err set means the agent
// did not answer, or answered nothing usable.
type Snapshot struct {
	Backend agent.Backend
	Keys    []*sshagent.Key
	Err     error
}

// agentLine names the agent and its state in a few words, for the tray's
// heading and the main window. managed reports whether sshush starts and
// stops it, rather than it being someone else's agent.
func agentLine(reachable, managed bool, b agent.Backend) string {
	switch {
	case !reachable && managed:
		return "Agent not running"
	case !reachable:
		return "Agent not reachable"
	case b.Mode == "vault" && b.VaultLocked:
		return "Vault agent, locked"
	case b.Mode == "vault":
		return "Vault agent, unlocked"
	case b.SpeaksOps:
		return "Key agent, keys in memory"
	}
	return "External agent"
}

// ViewOf maps a Snapshot to what the tray shows. managed reports whether the
// agent is one sshush starts and stops, rather than an external agent.
func ViewOf(s Snapshot, managed bool) View {
	v := View{Heading: agentLine(s.Err == nil, managed, s.Backend)}
	switch {
	case s.Err != nil:
		v.Icon = IconUnreachable
		v.Tooltip = "sshush: " + strings.ToLower(v.Heading)
		return v
	case s.Backend.Mode == "vault" && s.Backend.VaultLocked:
		v.Icon = IconLocked
		v.Tooltip = "sshush: vault locked"
		return v
	}
	v.Icon, v.Keys = IconKeys, keyItems(s.Keys)
	v.Tooltip = fmt.Sprintf("sshush: %s", plural(len(v.Keys), "key"))
	if s.Backend.Mode == "vault" {
		v.Icon = IconUnlocked
		v.Tooltip = fmt.Sprintf("sshush: vault unlocked, %s", plural(len(v.Keys), "key"))
	}
	if len(v.Keys) == 0 {
		v.Status = "No keys loaded"
	}
	return v
}

// keyItems labels each key "comment  SHA256:…", falling back to the key type
// when the comment is empty.
func keyItems(keys []*sshagent.Key) []KeyItem {
	items := make([]KeyItem, 0, len(keys))
	for _, k := range keys {
		name := k.Comment
		if name == "" {
			name = k.Format
		}
		item := KeyItem{Label: name + "  unparseable key"}
		if pub, err := ssh.ParsePublicKey(k.Blob); err == nil {
			item.Fingerprint = ssh.FingerprintSHA256(pub)
			item.Label = name + "  " + item.Fingerprint
		}
		items = append(items, item)
	}
	return items
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
