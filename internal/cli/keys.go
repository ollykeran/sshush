package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/ollykeran/sshush/internal/style"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// ListKeys prints the keyring's keys to stdout.
func ListKeys(keyring sshagent.Agent) error {
	return ListKeysTo(keyring, os.Stdout)
}

// AppendKeysTo appends the keyring's key lines to an existing Output builder.
func AppendKeysTo(keyring sshagent.Agent, out *style.Output) error {
	keys, err := keyring.List()
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		out.Warn("no keys loaded")
		return nil
	}
	maxTypeLen := 0
	for _, key := range keys {
		if l := len(key.Type()); l > maxTypeLen {
			maxTypeLen = l
		}
	}
	for _, key := range keys {
		out.Add(style.Pink(fmt.Sprintf("%-*s  %s  %s", maxTypeLen, key.Type(), ssh.FingerprintSHA256(key), key.Comment)))
	}
	return nil
}

// ListKeysTo writes the keyring's keys to w. Used for tests.
func ListKeysTo(keyring sshagent.Agent, w io.Writer) error {
	keys, err := keyring.List()
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		style.NewOutput().Warn("no keys loaded").PrintTo(w)
		return nil
	}
	maxTypeLen := 0
	for _, key := range keys {
		if l := len(key.Type()); l > maxTypeLen {
			maxTypeLen = l
		}
	}
	out := style.NewOutput()
	for _, key := range keys {
		out.Add(style.Pink(fmt.Sprintf("%-*s  %s  %s", maxTypeLen, key.Type(), ssh.FingerprintSHA256(key), key.Comment)))
	}
	out.PrintTo(w)
	return nil
}

type diffEntry struct {
	fp      string
	comment string
	keyType string
}

func agentKeysToEntries(keys []*sshagent.Key) []diffEntry {
	entries := make([]diffEntry, len(keys))
	for i, k := range keys {
		entries[i] = diffEntry{fp: ssh.FingerprintSHA256(k), comment: k.Comment, keyType: k.Type()}
	}
	return entries
}

// printKeysDiff returns an Output containing the diff between two key snapshots.
// Added (+), removed (-), and unchanged (=) keys are always shown.
func printKeysDiff(before, after []diffEntry) *style.Output {
	beforeByFP := make(map[string]diffEntry, len(before))
	for _, e := range before {
		beforeByFP[e.fp] = e
	}
	afterByFP := make(map[string]diffEntry, len(after))
	for _, e := range after {
		afterByFP[e.fp] = e
	}

	maxTypeLen := 0
	for _, e := range before {
		if l := len(e.keyType); l > maxTypeLen {
			maxTypeLen = l
		}
	}
	for _, e := range after {
		if l := len(e.keyType); l > maxTypeLen {
			maxTypeLen = l
		}
	}
	fmtStr := "%-*s  %s  %s"

	var added, removed, unchanged []string
	for fp, e := range afterByFP {
		line := fmt.Sprintf(fmtStr, maxTypeLen, e.keyType, fp, e.comment)
		if _, exists := beforeByFP[fp]; !exists {
			added = append(added, style.Green("+ "+line))
		} else {
			unchanged = append(unchanged, style.Pink("= "+line))
		}
	}
	for fp, e := range beforeByFP {
		if _, exists := afterByFP[fp]; !exists {
			line := fmt.Sprintf(fmtStr, maxTypeLen, e.keyType, fp, e.comment)
			removed = append(removed, style.Err("- "+line))
		}
	}

	out := style.NewOutput()
	if len(added) == 0 && len(removed) == 0 {
		out.Success("* sshushd no changes")
	}
	for _, e := range after {
		if _, existed := beforeByFP[e.fp]; !existed {
			out.Success("* sshushd key added: " + e.comment)
		}
	}
	for _, e := range before {
		if _, exists := afterByFP[e.fp]; !exists {
			out.Success("* sshushd key removed: " + e.comment)
		}
	}
	if len(added) > 0 || len(removed) > 0 {
		out.Spacer()
	}
	for _, s := range added {
		out.Add(s)
	}
	for _, s := range removed {
		out.Add(s)
	}
	for _, s := range unchanged {
		out.Add(s)
	}
	return out
}
