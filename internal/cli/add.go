package cli

import (
	"errors"
	"net"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/spf13/cobra"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func newAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [key-path...]",
		Short: "Add key(s) to the running agent",
		RunE:  runAdd,
	}
	cmd.Flags().StringSliceP("key", "k", nil, "Key file path(s) to add (can be repeated)")
	return cmd
}

func runAdd(cmd *cobra.Command, args []string) error {
	cfg := env.Config
	if cfg == nil {
		return errors.New(style.Err("config not loaded"))
	}
	paths := args
	if keys, _ := cmd.Flags().GetStringSlice("key"); len(keys) > 0 {
		paths = keys
	}
	if len(paths) == 0 {
		paths = cfg.KeyPaths
	}
	if len(paths) == 0 {
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
	client := sshagent.NewClient(conn)
	before, _ := client.List()
	for _, path := range paths {
		if err := agent.AddKeyFromPath(client, path); err != nil {
			return err
		}
	}
	after, _ := client.List()
	printKeysDiff(agentKeysToEntries(before), agentKeysToEntries(after), false)
	return nil
}
