package cli

import (
	"io"
	"os"

	"github.com/ollykeran/sshush/internal/bridge"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/transport"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
)

func newRelayCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "relay",
		Short: "Connect stdin and stdout to the running agent",
		Long: "Carry the SSH agent protocol between stdin/stdout and the running agent's socket (on Windows, its named pipe).\n\n" +
			"This is the Windows half of using one agent from WSL: with [agent].type = \"windows\" in the distro's config, " +
			"the sshush there runs 'sshush.exe relay' for each connection to its own socket. It is run by sshush, not typed: " +
			"it prints nothing on success, and exits when either end closes.",
		Args: cobra.NoArgs,
		RunE: runRelay,
	}
}

func runRelay(cmd *cobra.Command, _ []string) error {
	socketPath, err := getSocketPath(configFrom(cmd))
	if err != nil {
		return style.NewOutput().Error("failed to get socket path").AsError()
	}
	return relayAgent(socketPath, os.Stdin, os.Stdout)
}

// relayAgent connects in and out to the agent at socketPath until either end
// is done.
func relayAgent(socketPath string, in io.Reader, out io.Writer) error {
	conn, err := transport.Dial(socketPath)
	if err != nil {
		// Stdout is the protocol stream, so this goes to stderr like every
		// other error, where the far side can pick it up to explain itself.
		return style.NewOutput().
			Error("no agent is running at " + utils.DisplayPath(socketPath)).
			Info("Start it with 'sshush start'.").
			AsError()
	}
	defer conn.Close()
	bridge.Relay(conn, in, out)
	return nil
}
