package cli

import (
	"github.com/ollykeran/sshush/internal/style"
	"github.com/spf13/cobra"
)

func newLockCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "lock",
		Short: "Lock the agent (not implemented)",
		RunE:  runLock,
	}
}

func runLock(cmd *cobra.Command, _ []string) error {
	return style.NewOutput().Error("lock: not implemented").AsError()
}
