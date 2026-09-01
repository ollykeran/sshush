package cli

import (
	"strings"

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
		msg := err.Error()
		if msg == "agent: failure" {
			msg = "unlock failed: wrong passphrase, or the running agent is not a vault (run 'sshush start' after setting [vault].vault_path in config)"
		} else {
			msg = "unlock failed: " + msg
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
		msg := err.Error()
		// The keyring's own "agent: not locked" and "agent: incorrect passphrase"
		// never reach here: ServeAgent sends a bare failure byte and the client
		// synthesises "agent: failure", so both cases land in the fallback below.
		if msg == "agent: not locked" {
			style.NewOutput().Info("Agent is already unlocked.").PrintErr()
			return nil
		}
		if strings.Contains(msg, "incorrect passphrase") {
			return style.NewOutput().Error("unlock failed: wrong passphrase").AsError()
		}
		return style.NewOutput().Error("unlock failed: " + msg).AsError()
	}
	style.NewOutput().Success("Agent unlocked.").PrintErr()
	return nil
}
