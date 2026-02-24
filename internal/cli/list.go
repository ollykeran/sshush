package cli

import (
	"errors"
	"net"

	"github.com/ollykeran/sshush/internal/style"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh/agent"
)

func newListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List keys in the running agent",
		RunE:  runList,
	}
}

func runList(cmd *cobra.Command, _ []string) error {
	cfg := env.Config
	if cfg == nil {
		return errors.New(style.Err("config not loaded"))
	}
	socketPath, err := getSocketPath()
	if err != nil {
		return err
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	client := agent.NewClient(conn)
	return ListKeys(client)
}
