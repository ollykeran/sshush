package cli

import (
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
	return style.NewOutput().Error("unlock: not implemented").AsError()
}
