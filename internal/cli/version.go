package cli

import (
	"fmt"
	"runtime"

	"github.com/ollykeran/sshush/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Example: "sshush version",
		Long:    "Print the sshush version and exit.",
		Short:   "Print the sshush version",
		Args:    argsNoneOrHelp,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("sshush %s (%s)\n", version.Version, runtime.Version())
		},
	}
}
