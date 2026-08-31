package cli

import (
	"fmt"
	"os"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/spf13/cobra"
)

func newListCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List keys in the running agent",
		Example: "sshush list",
		Long:    "List keys in the running agent via the agent socket. This command is not affected by config.",
		Args:    argsNoneOrHelp,
		RunE:    runList,
	}
}

func runList(cmd *cobra.Command, _ []string) error {
	if env.Config == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}
	socketPath, err := getSocketPath()
	if err != nil {
		return fmt.Errorf("cli: get socket path: %w", err)
	}
	session, err := agent.Open(socketPath)
	if err != nil {
		return fmt.Errorf("cli: list keys from socket: %w", err)
	}
	defer session.Close()
	keys, err := session.List()
	if err != nil {
		return fmt.Errorf("cli: list keys from socket: %w", err)
	}
	return ListKeysSnapshotTo(keys, os.Stdout)
}
