package cli

import (
	"errors"
	"net"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/spf13/cobra"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func newLockCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "lock",
		Short: "Lock the agent (vault or keys mode)",
		Long: "Connect to the running agent and lock it. For a vault agent, wipes the master key from memory. " +
			"For a keys-mode agent, set a passphrase lock (you confirm twice) so keys cannot sign until unlock.",
		Args: cobra.NoArgs,
		RunE: runLock,
	}
}

func runLock(cmd *cobra.Command, _ []string) error {
	if env.Config == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}
	socketPath, err := getSocketPath()
	if err != nil {
		return style.NewOutput().Error("failed to get socket path").AsError()
	}
	mode, live := agent.LiveBackendMode(socketPath)
	if !live {
		return style.NewOutput().Error("cannot connect to agent (is sshush running?)").AsError()
	}
	switch mode {
	case "vault":
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
	case "keys":
		passphrase, err := ReadPassphraseWithConfirm("Passphrase: ", "Confirm passphrase: ")
		if err != nil {
			if errors.Is(err, ErrPassphrasesDoNotMatch) {
				return style.NewOutput().Error("passphrases do not match").AsError()
			}
			return style.NewOutput().Error("read passphrase: " + err.Error()).AsError()
		}
		defer ClearBytes(passphrase)
		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			return style.NewOutput().Error("cannot connect to agent: " + err.Error()).AsError()
		}
		defer conn.Close()
		client := sshagent.NewClient(conn)
		if err := client.Lock(passphrase); err != nil {
			return style.NewOutput().Error("lock failed: " + err.Error()).AsError()
		}
		style.NewOutput().Success("Agent locked.").PrintErr()
		return nil
	default:
		return style.NewOutput().Error("unexpected agent backend").AsError()
	}
}
