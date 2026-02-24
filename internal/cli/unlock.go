package cli

import (
	"errors"

	"github.com/ollykeran/sshush/internal/style"
	"github.com/spf13/cobra"
)

func newUnlockCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "unlock",
		Short: "Unlock the agent (not implemented)",
		RunE:  runUnlock,
	}
}

func runUnlock(cmd *cobra.Command, _ []string) error {
	return errors.New(style.Err("unlock: not implemented"))
}
