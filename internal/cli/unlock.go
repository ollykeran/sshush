package cli

import (
	"errors"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/spf13/cobra"
)

func newUnlockCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "unlock",
		Short: "Unlock the agent with passphrase",
		Long: "Connect to the running agent and unlock it. For a vault agent, use the master passphrase. " +
			"For a keys-mode agent, use the passphrase you set when locking.",
		Args: cobra.NoArgs,
		RunE: runUnlock,
	}
}

func runUnlock(cmd *cobra.Command, _ []string) error {
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
		return runUnlockVault(session, backend)
	case "keys":
		return runUnlockKeys(session)
	default:
		return style.NewOutput().Error("unexpected agent backend").AsError()
	}
}

func runUnlockVault(session *agent.Session, backend agent.Backend) error {
	if !backend.VaultLocked {
		style.NewOutput().Info("Vault is already unlocked.").PrintErr()
		return nil
	}
	passphrase, err := readPassphrase("Passphrase: ")
	if err != nil {
		return style.NewOutput().Error("read passphrase: " + err.Error()).AsError()
	}
	defer ClearBytes(passphrase)
	if err := session.Unlock(passphrase); err != nil {
		var msg string
		switch {
		case errors.Is(err, agent.ErrWrongPassphrase):
			msg = "unlock failed: wrong passphrase"
		case errors.Is(err, agent.ErrNotLocked):
			style.NewOutput().Info("Vault is already unlocked.").PrintErr()
			return nil
		// An older sshushd sends no reason byte, so this opaque string is all
		// there is. Keep it as the last resort after the typed cases above.
		case err.Error() == "agent: failure":
			// An older sshushd cannot tell us which of these it was.
			msg = "unlock failed: wrong passphrase, or the running agent is not a vault (run 'sshush start' after setting [vault].vault_path in config)"
		default:
			msg = "unlock failed: " + err.Error()
		}
		return style.NewOutput().Error(msg).AsError()
	}
	style.NewOutput().Success("Vault unlocked.").PrintErr()
	return nil
}

func runUnlockKeys(session *agent.Session) error {
	passphrase, err := readPassphrase("Passphrase: ")
	if err != nil {
		return style.NewOutput().Error("read passphrase: " + err.Error()).AsError()
	}
	defer ClearBytes(passphrase)
	if err := session.Unlock(passphrase); err != nil {
		switch {
		case errors.Is(err, agent.ErrNotLocked):
			style.NewOutput().Info("Agent is already unlocked.").PrintErr()
			return nil
		case errors.Is(err, agent.ErrWrongPassphrase):
			return style.NewOutput().Error("unlock failed: wrong passphrase").AsError()
		}
		return style.NewOutput().Error("unlock failed: " + err.Error()).AsError()
	}
	style.NewOutput().Success("Agent unlocked.").PrintErr()
	return nil
}
