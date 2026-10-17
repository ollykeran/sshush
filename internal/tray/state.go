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
// identically, so the tray redraws only when the View changes.
type View struct {
	Icon    Icon
	Tooltip string
	// Keys holds one label per identity the agent serves, in agent order.
	Keys []string
	// Status, when not empty, stands in for Keys: why there is nothing to list.
	Status    string
	CanUnlock bool
	CanLock   bool
}

// Equal reports whether v and o render identically.
func (v View) Equal(o View) bool {
	if v.Icon != o.Icon || v.Tooltip != o.Tooltip || v.Status != o.Status ||
		v.CanUnlock != o.CanUnlock || v.CanLock != o.CanLock || len(v.Keys) != len(o.Keys) {
		return false
	}
	for i := range v.Keys {
		if v.Keys[i] != o.Keys[i] {
			return false
		}
	}
	return true
}

// Snapshot is what one poll learned from the agent. Err set means the agent
// could not be reached or probed, and the other fields are meaningless.
type Snapshot struct {
	Backend agent.Backend
	Keys    []*sshagent.Key
	Err     error
}

// ViewOf maps a Snapshot to what the tray shows.
func ViewOf(s Snapshot) View {
	if s.Err != nil {
		return View{Icon: IconUnreachable, Tooltip: "sshush: agent not running", Status: "Agent not running"}
	}
	if s.Backend.Mode == "vault" && s.Backend.VaultLocked {
		return View{Icon: IconLocked, Tooltip: "sshush: vault locked", Status: "Vault locked", CanUnlock: true}
	}
	v := View{Icon: IconKeys, Keys: keyLabels(s.Keys)}
	if s.Backend.Mode == "vault" {
		v.Icon = IconUnlocked
		v.CanLock = true
	}
	v.Tooltip = fmt.Sprintf("sshush: %s", plural(len(v.Keys), "key"))
	if s.Backend.Mode == "vault" {
		v.Tooltip = fmt.Sprintf("sshush: vault unlocked, %s", plural(len(v.Keys), "key"))
	}
	if len(v.Keys) == 0 {
		v.Status = "No keys loaded"
	}
	return v
}

// keyLabels renders each key as "comment  SHA256:…", falling back to the key
// type when the comment is empty.
func keyLabels(keys []*sshagent.Key) []string {
	labels := make([]string, 0, len(keys))
	for _, k := range keys {
		name := k.Comment
		if name == "" {
			name = k.Format
		}
		fp := "unparseable key"
		if pub, err := ssh.ParsePublicKey(k.Blob); err == nil {
			fp = ssh.FingerprintSHA256(pub)
		}
		labels = append(labels, name+"  "+fp)
	}
	return labels
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
