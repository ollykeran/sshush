package cli

import (
	"errors"

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
	return errors.New(style.Err("lock: not implemented"))
}
