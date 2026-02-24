package cli

import (
	"github.com/spf13/cobra"
)

func newServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Load keys and run the SSH agent (default)",
		RunE:  runServe,
	}
}

func runServe(cmd *cobra.Command, _ []string) error {
	return runStartDaemon(cmd)
}
