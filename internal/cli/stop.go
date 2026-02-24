package cli

import (
	"errors"
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
		RunE:  runStop,
	}
}

func runStop(cmd *cobra.Command, _ []string) error {
	pidFilePath := utils.PidFilePath()
	if err := stopDaemon(pidFilePath); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, style.Green("sshushd stopped"))
	return nil
}

// stopDaemon sends SIGTERM to the process in pidFilePath and waits for it to exit.
func stopDaemon(pidFilePath string) error {
	data, err := os.ReadFile(pidFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New(style.Err("no pidfile at " + pidFilePath + " (daemon may not be running)"))
		}
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("%s: %w", style.Err("invalid pidfile"), err)
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("%s: %w", style.Err(fmt.Sprintf("find process %d", pid)), err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("%s: %w", style.Err("send SIGTERM"), err)
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
