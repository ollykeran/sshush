package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
)

func newStartCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the sshush agent daemon",
		RunE:  runStart,
	}
}

func runStart(cmd *cobra.Command, _ []string) error {
	return runStartDaemon(cmd)
}

// runStartDaemon resolves config, starts the sshushd binary with SSHUSH_CONFIG, and waits for the socket.
// Used by both start and serve commands.
func runStartDaemon(cmd *cobra.Command) error {
	configPath, err := utils.ResolveConfigPath(cmd)
	if err != nil {
		return err
	}
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	cfg, err := config.LoadConfig(absConfigPath)
	if err != nil {
		return err
	}
	pidFilePath := utils.PidFilePath()
	if _, err := os.Stat(pidFilePath); err == nil {
		return errors.New(style.Err("sshushd already running (pidfile "+pidFilePath+" exists)") + "\n" + style.Pink("use 'sshush reload' to apply config changes"))
	}
	if sshushd.CheckAlreadyRunning(cfg.SocketPath) {
		return errors.New(style.Err("agent already running at " + cfg.SocketPath))
	}
	sshushdPath, err := findSshushdBinary()
	if err != nil {
		return err
	}
	child := exec.Command(sshushdPath)
	child.Env = append(os.Environ(), "SSHUSH_CONFIG="+absConfigPath)
	child.Stdin = nil
	child.Stdout = nil
	child.Stderr = nil
	if err := child.Start(); err != nil {
		return fmt.Errorf("%s: %w", style.Err("start sshushd"), err)
	}
	if sshushd.WaitForSocket(cfg.SocketPath, 50, 10*time.Millisecond) {
		fmt.Fprintln(os.Stderr, style.Green("sshush agent started at "+cfg.SocketPath))
		return nil
	}
	done := make(chan struct{})
	go func() {
		child.Wait()
		close(done)
	}()
	select {
	case <-done:
		return errors.New(style.Err("sshushd exited before socket was ready"))
	case <-time.After(100 * time.Millisecond):
	}
	fmt.Fprintln(os.Stderr, style.Green("sshush agent started at "+cfg.SocketPath))
	return nil
}

func findSshushdBinary() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(execPath)
	sshushdPath := filepath.Join(dir, "sshushd")
	if _, err := os.Stat(sshushdPath); err == nil {
		return sshushdPath, nil
	}
	path, err := exec.LookPath("sshushd")
	if err == nil {
		return path, nil
	}
	return "", errors.New(style.Err("sshushd binary not found (looked in " + dir + " and PATH)"))
}
