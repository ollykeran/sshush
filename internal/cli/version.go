package cli

import (
	"fmt"

	"github.com/ollykeran/sshush/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "version",
		Example: "sshush version\nsshush version --check",
		Long:    "Print the sshush version and exit. With --check, also check GitHub for a newer release.",
		Short:   "Print the sshush version",
		Args:    argsNoneOrHelp,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version.Line("sshush"))

			if check, _ := cmd.Flags().GetBool("check"); check {
				msg, err := version.CheckLatest()
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Version check failed: %v\n", err)
					return
				}
				if msg != "" {
					fmt.Fprintln(cmd.ErrOrStderr(), msg)
				} else {
					fmt.Fprintln(cmd.ErrOrStderr(), "sshush is up to date")
				}
			}
		},
	}
	cmd.Flags().Bool("check", false, "check GitHub for a newer version")
	return cmd
}
