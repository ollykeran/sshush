package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ollykeran/sshush/internal/style"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// ListKeys prints the keyring's keys (fingerprint + comment) to stdout.
func ListKeys(keyring sshagent.Agent) error {
	return ListKeysTo(keyring, os.Stdout)
}

// ListKeysTo writes the keyring's keys to w. Used for tests.
func ListKeysTo(keyring sshagent.Agent, w io.Writer) error {
	keys, err := keyring.List()
	if err != nil {
		return err
	}
	var lines []string
	for _, key := range keys {
		lines = append(lines, style.Pink(ssh.FingerprintSHA256(key)+" "+key.Comment))
	}
	_, err = fmt.Fprintln(w, style.Box(strings.Join(lines, "\n")))
	return err
}

type diffEntry struct {
	fp      string
	comment string
}

func agentKeysToEntries(keys []*sshagent.Key) []diffEntry {
	entries := make([]diffEntry, len(keys))
	for i, k := range keys {
		entries[i] = diffEntry{fp: ssh.FingerprintSHA256(k), comment: k.Comment}
	}
	return entries
}

// printKeysDiff prints a diff between two snapshots of agent keys.
// When showUnchanged is true, keys present in both sets are always shown with "=".
// When showUnchanged is false, "=" lines are shown only if there are no +/- changes
// (so the box is never empty).
func printKeysDiff(before, after []diffEntry, showUnchanged bool) {
	beforeByFP := make(map[string]diffEntry, len(before))
	for _, e := range before {
		beforeByFP[e.fp] = e
	}
	afterByFP := make(map[string]diffEntry, len(after))
	for _, e := range after {
		afterByFP[e.fp] = e
	}

	var added, removed, unchanged []string
	for fp, e := range afterByFP {
		if _, exists := beforeByFP[fp]; !exists {
			added = append(added, style.Green(fmt.Sprintf("+ %s %s", fp, e.comment)))
		} else {
			unchanged = append(unchanged, style.Pink(fmt.Sprintf("= %s %s", fp, e.comment)))
		}
	}
	for fp, e := range beforeByFP {
		if _, exists := afterByFP[fp]; !exists {
			removed = append(removed, style.Err(fmt.Sprintf("- %s %s", fp, e.comment)))
		}
	}

	var lines []string
	lines = append(lines, added...)
	lines = append(lines, removed...)
	if showUnchanged || len(lines) == 0 {
		lines = append(lines, unchanged...)
	}
	if len(lines) > 0 {
		fmt.Println(style.Box(strings.Join(lines, "\n")))
	}
}
