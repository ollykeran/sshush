package cli

import (
	"errors"
	"net"
	"strings"

	"github.com/ollykeran/sshush/internal/style"
	"github.com/spf13/cobra"
	ssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func newRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove [fingerprint|comment...]",
		Short: "Remove key(s) from the running agent by fingerprint or comment",
		RunE:  runRemove,
	}
	cmd.Flags().StringSliceP("key", "k", nil, "Fingerprint(s) or comment(s) to remove (can be repeated)")
	return cmd
}

func runRemove(cmd *cobra.Command, args []string) error {
	cfg := env.Config
	if cfg == nil {
		return errors.New(style.Err("config not loaded"))
	}
	idents := args
	if keys, _ := cmd.Flags().GetStringSlice("key"); len(keys) > 0 {
		idents = keys
	}
	if len(idents) == 0 {
		cmd.Usage()
		return nil
	}
	socketPath, err := getSocketPath()
	if err != nil {
		return err
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	client := agent.NewClient(conn)
	before, err := client.List()
	if err != nil {
		return err
	}
	want := make(map[string]bool)
	for _, a := range idents {
		want[strings.TrimSpace(a)] = true
	}
	for _, key := range before {
		fp := ssh.FingerprintSHA256(key)
		if want[fp] || want[key.Comment] {
			if err := client.Remove(key); err != nil {
				return err
			}
		}
	}
	after, _ := client.List()
	printKeysDiff(agentKeysToEntries(before), agentKeysToEntries(after), false)
	return nil
}
