package cli

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func newStartCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the sshush agent daemon",
		Long:  "Start the sshush agent daemon in the background.\nUsage: eval $(sshush start)",
		Args:  argsNoneOrHelp,
		RunE:  runStart,
	}
	cmd.Flags().StringP("config", "c", "", "path to config file")
	return cmd
}

func runStart(cmd *cobra.Command, _ []string) error {
	return runStartDaemon(cmd)
}

// runStartDaemon resolves config, starts the sshushd binary with SSHUSH_CONFIG, and waits for the socket.
// On success the export line goes to stdout (for eval) and all other output goes to stderr.
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
	if sshushd.CheckAlreadyRunning(cfg.SocketPath) {
		absSocket, _ := filepath.Abs(cfg.SocketPath)
		if !isTTY(os.Stdout) {
			fmt.Fprintln(os.Stdout, "export SSH_AUTH_SOCK='"+absSocket+"'")
		}
		out := style.NewOutput().
			Success("* sshushd running at " + absSocket)
		conn, err := net.Dial("unix", cfg.SocketPath)
		if err == nil {
			defer conn.Close()
			out.Spacer()
			AppendKeysTo(sshagent.NewClient(conn), out)
		}
		out.PrintErr()
		return nil
	}

	out := style.NewOutput()
	loadable := 0
	for _, kp := range cfg.KeyPaths {
		if _, err := os.Stat(kp); err != nil {
			out.Warn("key not found: " + kp)
		} else {
			loadable++
		}
	}
	if loadable == 0 {
		out.Error("no keys will be loaded")
	}

	pidFilePath := utils.PidFilePath()
	if _, err := os.Stat(pidFilePath); err == nil {
		return style.NewOutput().
			Error("sshushd already running (pidfile " + pidFilePath + " exists)").
			Info("use 'sshush reload' to apply config changes").
			AsError()
	}

	if err := sshushd.StartDaemon(absConfigPath, cfg.SocketPath); err != nil {
		return style.NewOutput().Error(err.Error()).AsError()
	}
	return startSuccess(out, cfg.SocketPath)
}

// startSuccess prints the export line to stdout (for eval) only when stdout is
// piped, and the pretty success message (and any prior warnings) to stderr.
func startSuccess(out *style.Output, socketPath string) error {
	absSocket, _ := filepath.Abs(socketPath)

	if !isTTY(os.Stdout) {
		fmt.Fprintln(os.Stdout, "export SSH_AUTH_SOCK='"+absSocket+"'")
	}

	if out.Len() > 0 {
		out.Spacer()
	}
	out.Success("* sshushd started with socket: " + absSocket)

	conn, err := net.Dial("unix", socketPath)
	if err == nil {
		defer conn.Close()
		out.Spacer()
		AppendKeysTo(sshagent.NewClient(conn), out)
	}

	out.PrintErr()
	return nil
}

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
