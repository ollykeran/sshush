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

	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/platform"
	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/server"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
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
	// The server asks the agent per connection, so it can start without one: it
	// authorizes nobody until the agent is up, and needs no restart once it is.
	// Worth saying out loud, though, since nothing else would explain the refusals.
	agentDown := cfg.ServerAuthorizedKeys == "" && !sshushd.CheckAlreadyRunning(cfg.SocketPath)
	configPath, err := runtime.ResolveConfigPath(cmd)
	if err != nil {
		return fmt.Errorf("cli: resolve config path: %w", err)
	}
	if err := sshushd.StartServerDaemon(configPath, int(cfg.ServerListenPort)); err != nil {
		if err.Error() == "sshushd: server already running on port "+fmt.Sprint(cfg.ServerListenPort) {
			style.NewOutput().Success("SSH server is already running on port " + fmt.Sprint(cfg.ServerListenPort)).PrintErr()
			return nil
		}
		return style.NewOutput().Error(err.Error()).AsError()
	}
	out := style.NewOutput().Success("SSH server started on port " + fmt.Sprint(cfg.ServerListenPort))
	if agentDown {
		out.Warn("No agent is running, so no key can be authorized yet.")
		out.Info(startAgentHint(cfg))
	}
	out.Print()
	return nil
}

// startAgentHint says how to get an agent up, which differs when the agent is
// somebody else's.
func startAgentHint(cfg config.Config) string {
	if !cfg.IsExternal() {
		return "Start the agent with 'sshush start'; the server picks it up on the next connection."
	}
	if cfg.SocketPath == "" {
		return "[agent].type = \"external\" but no socket found; set [agent].socket_path or export SSH_AUTH_SOCK."
	}
	return "[agent].type = \"external\": start your external agent at " + cfg.SocketPath + "."
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
		return fmt.Errorf("cli: resolve config path: %w", err)
	}
	cfg, err := LoadMergedConfig(configPath, LoadOverrides{})
	if err != nil {
		return fmt.Errorf("cli: load merged config: %w", err)
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
	out.Add(statusLabel("port") + style.Success(fmt.Sprintf("%d", cfg.ServerListenPort)))

	if cfg.ServerAuthorizedKeys != "" {
		out.Add(statusLabel("auth") + style.Success("authorized_keys "+utils.DisplayPath(cfg.ServerAuthorizedKeys)+"  ✓"))
	} else if sshushd.CheckAlreadyRunning(cfg.SocketPath) {
		out.Add(statusLabel("auth") + style.Success("agent "+utils.DisplayPath(cfg.SocketPath)+"  ✓"))
	} else {
		out.Add(statusLabel("auth") + style.Warn("agent "+utils.DisplayPath(cfg.SocketPath)+" is not running"))
		out.Add(statusLabel("") + style.Warn("no key can be authorized until it is"))
	}

	hostKeyPath := platform.ServerHostKeyPath(cfg.ServerHostKey)
	fingerprint, fpErr := server.HostKeyFingerprint(hostKeyPath)
	if fpErr == nil {
		out.Add(statusLabel("host key") + style.Success(utils.DisplayPath(hostKeyPath)+"  ✓"))
		out.Add(statusLabel("fingerprint") + style.Text(fingerprint))
	} else {
		out.Add(statusLabel("host key") + style.Warn(utils.DisplayPath(hostKeyPath)+" (created on first start)"))
	}

	if processRunning {
		out.Add(statusLabel("process") + style.Success(fmt.Sprintf("running (PID %d)  ✓", pid)))
	} else {
		out.Add(statusLabel("process") + style.Err("not running  ✗"))
	}

	if dialErr == nil {
		out.Add(statusLabel("connection") + style.Success("ok  ✓"))
	} else {
		out.Add(statusLabel("connection") + style.Err("✗ "+dialErr.Error()))
		if processRunning {
			out.Add(statusLabel("") + style.Err("pidfile exists but the port is not reachable"))
		}
	}
	out.Print()
	return nil
}

// statusLabel renders a status line's label, padded so the values line up. An
// empty name gives the blank label a continuation line hangs from.
func statusLabel(name string) string {
	if name != "" {
		name += ":"
	}
	return style.Focus(fmt.Sprintf("%-*s", statusLabelWidth, name))
}

// statusLabelWidth fits the longest label plus a space.
const statusLabelWidth = len("fingerprint:") + 1

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
