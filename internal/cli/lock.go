package cli

import (
	"errors"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/spf13/cobra"
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
	session, err := agent.Open(socketPath)
	if err != nil {
		return style.NewOutput().Error("cannot connect to agent (is sshush running?)").AsError()
	}
	defer session.Close()
	backend, err := session.Backend()
	if err != nil {
		return style.NewOutput().Error("cannot connect to agent (is sshush running?)").AsError()
	}
	switch backend.Mode {
	case "vault":
		if backend.VaultLocked {
			style.NewOutput().Info("Vault is already locked.").PrintErr()
			return nil
		}
		if err := session.Lock(nil); err != nil {
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
		if err := session.Lock(passphrase); err != nil {
			return style.NewOutput().Error("lock failed: " + err.Error()).AsError()
		}
		style.NewOutput().Success("Agent locked.").PrintErr()
		return nil
	default:
		return style.NewOutput().Error("unexpected agent backend").AsError()
	}
}
