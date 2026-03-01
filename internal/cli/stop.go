package cli

import (
	"errors"
	"os"

	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/spf13/cobra"
)

func newStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the sshush agent daemon",
		Args:  argsNoneOrHelp,
		RunE:  runStop,
	}
}

func runStop(cmd *cobra.Command, _ []string) error {
	pidFilePath := runtime.PidFilePath()
	if err := sshushd.StopDaemon(pidFilePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return style.NewOutput().
				Error("no pidfile at " + pidFilePath).
				Info("daemon may not be running").
				AsError()
		}
		return style.NewOutput().Error(err.Error()).AsError()
	}
	style.NewOutput().Success("sshushd stopped").Print()
	return nil
}
