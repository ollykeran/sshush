package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
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
	pidFilePath := utils.PidFilePath()
	if err := stopDaemon(pidFilePath); err != nil {
		return err
	}
	style.NewOutput().Success("sshushd stopped").Print()
	return nil
}

// stopDaemon sends SIGTERM to the process in pidFilePath and waits for it to exit.
func stopDaemon(pidFilePath string) error {
	data, err := os.ReadFile(pidFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return style.NewOutput().
				Error("no pidfile at " + pidFilePath).
				Info("daemon may not be running").
				AsError()
		}
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return style.NewOutput().Error(fmt.Sprintf("invalid pidfile: %v", err)).AsError()
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return style.NewOutput().Error(fmt.Sprintf("find process %d: %v", pid, err)).AsError()
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return style.NewOutput().Error(fmt.Sprintf("send SIGTERM: %v", err)).AsError()
	}
	for i := 0; i < 50; i++ {
		if process.Signal(syscall.Signal(0)) != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	os.Remove(pidFilePath)
	return nil
}
