package cli

import (
	"fmt"
	"runtime"

	"github.com/ollykeran/sshush/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the sshush version",
		Args:  argsNoneOrHelp,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("sshush %s (%s)\n", version.Version, runtime.Version())
		},
	}
}
