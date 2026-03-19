package cli

import (
	"net"

	"github.com/ollykeran/sshush/internal/style"
	"github.com/spf13/cobra"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func newLockCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "lock",
		Short: "Lock the vault",
		Long:  "Connect to the running agent and lock the vault (wipe master key from memory). Only applies when [vault].vault_path is set in config.",
		Args:  cobra.NoArgs,
		RunE:  runLock,
	}
}

func runLock(cmd *cobra.Command, _ []string) error {
	if env.Config == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}
	socketPath := env.Config.SocketPath
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return style.NewOutput().Error("cannot connect to agent: " + err.Error()).AsError()
	}
	defer conn.Close()
	client := sshagent.NewClient(conn)
	if err := client.Lock(nil); err != nil {
		return style.NewOutput().Error("lock failed: " + err.Error()).AsError()
	}
	style.NewOutput().Success("Vault locked.").PrintErr()
	return nil
}
