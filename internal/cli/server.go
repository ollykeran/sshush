package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/spf13/cobra"
)

func newServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "server",
		Aliases: []string{"serve"},
		Short:   "Start the SSH server daemon",
		Long:    "Starts the TCP SSH server daemon (separate process) on the port set in [server].listen_port. For agent-backed auth, start the agent first with 'sshush start'.",
		Args:    argsNoneOrHelp,
		RunE:    runServer,
	}
	cmd.Flags().StringP("config", "c", "", "path to config file")
	cmd.AddCommand(newServerStatusCommand())
	cmd.AddCommand(newServerStopCommand())
	return cmd
}

func runServer(cmd *cobra.Command, _ []string) error {
	if env.Config == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}
	cfg := *env.Config
	if cfg.ServerListenPort <= 0 {
		return style.NewOutput().
			Error("SSH server is not enabled.").
			Info("Set [server].listen_port in config (e.g. listen_port = 2222) then run 'sshush server'.").
			AsError()
	}
	if cfg.ServerAuthorizedKeys == "" && !sshushd.CheckAlreadyRunning(cfg.SocketPath) {
		return style.NewOutput().
			Error("Agent not running.").
			Info("Start the agent first with 'sshush start'.").
			AsError()
	}
	configPath, err := runtime.ResolveConfigPath(cmd)
	if err != nil {
		return err
	}
	if err := sshushd.StartServerDaemon(configPath, int(cfg.ServerListenPort)); err != nil {
		if err.Error() == "already running" {
			style.NewOutput().Success("SSH server is already running on port " + fmt.Sprint(cfg.ServerListenPort)).PrintErr()
			return nil
		}
		return style.NewOutput().Error(err.Error()).AsError()
	}
	style.NewOutput().Success("SSH server started on port " + fmt.Sprint(cfg.ServerListenPort)).Print()
	return nil
}

func newServerStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show SSH server status and test connection",
		Long:  "Check if the SSH server daemon is running (via pidfile), then test TCP connection to [server].listen_port.",
		Args:  argsNoneOrHelp,
		RunE:  runServerStatus,
	}
}

func runServerStatus(cmd *cobra.Command, _ []string) error {
	configPath, err := runtime.ResolveConfigPath(cmd)
	if err != nil {
		return err
	}
	cfg, err := LoadMergedConfig(configPath, LoadOverrides{})
	if err != nil {
		return err
	}
	if cfg.ServerListenPort <= 0 {
		style.NewOutput().
			Error("SSH server is not enabled ([server].listen_port not set or 0)").
			Info("Set [server].listen_port in config (e.g. listen_port = 2222) then run 'sshush server'.").
			Print()
		return nil
	}

	pidFilePath := runtime.ServerPidFilePath()
	var processRunning bool
	var pid int
	data, err := os.ReadFile(pidFilePath)
	if err == nil {
		pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		if pid > 0 {
			if p, findErr := os.FindProcess(pid); findErr == nil && p.Signal(syscall.Signal(0)) == nil {
				processRunning = true
			}
		}
	}

	addr := "127.0.0.1:" + strconv.Itoa(int(cfg.ServerListenPort))
	conn, dialErr := net.DialTimeout("tcp", addr, 2*time.Second)
	if dialErr == nil {
		conn.Close()
	}

	out := style.NewOutput()
	out.Info(fmt.Sprintf("port: %d", cfg.ServerListenPort))
	if processRunning {
		out.Info(fmt.Sprintf("process: running (PID %d)", pid))
	} else {
		out.Info("process: not running")
	}
	if dialErr != nil {
		out.Info("connection: failed (" + dialErr.Error() + ")")
		if processRunning {
			out.Info("(process has pidfile but port not reachable)")
		}
	} else {
		out.Info("connection: ok")
	}
	out.Print()
	return nil
}

func newServerStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the SSH server daemon",
		Long:  "Stop the TCP SSH server daemon by sending SIGTERM and removing its pidfile.",
		Args:  argsNoneOrHelp,
		RunE:  runServerStop,
	}
}

func runServerStop(_ *cobra.Command, _ []string) error {
	pidFilePath := runtime.ServerPidFilePath()
	if err := sshushd.StopDaemon(pidFilePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return style.NewOutput().
				Error("no pidfile at " + pidFilePath).
				Info("SSH server may not be running").
				AsError()
		}
		return style.NewOutput().Error(err.Error()).AsError()
	}
	style.NewOutput().Success("SSH server stopped").Print()
	return nil
}
