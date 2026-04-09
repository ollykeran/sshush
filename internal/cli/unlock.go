package cli

import (
	"errors"
	"net"
	"strings"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/vault"
	"github.com/spf13/cobra"
	sshagent "golang.org/x/crypto/ssh/agent"
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
	mode, live := agent.LiveBackendMode(socketPath)
	if !live {
		return style.NewOutput().Error("cannot connect to agent (is sshush running?)").AsError()
	}
	switch mode {
	case "vault":
		return runUnlockVault(socketPath)
	case "keys":
		return runUnlockKeys(socketPath)
	default:
		return style.NewOutput().Error("unexpected agent backend").AsError()
	}
}

func runUnlockVault(socketPath string) error {
	resp, extErr := agent.CallExtension(socketPath, vault.ExtensionVaultLocked, nil)
	if extErr != nil {
		if errors.Is(extErr, sshagent.ErrExtensionUnsupported) {
			return style.NewOutput().
				Error("this agent does not support vault status; use [agent].vault = true with [vault].vault_path and run 'sshush start'.").
				AsError()
		}
		return style.NewOutput().Error("vault status: " + extErr.Error()).AsError()
	}
	if len(resp) == 1 && resp[0] == 0 {
		style.NewOutput().Info("Vault is already unlocked.").PrintErr()
		return nil
	}
	if len(resp) != 1 || resp[0] != 1 {
		return style.NewOutput().Error("unexpected vault-locked response from agent").AsError()
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return style.NewOutput().Error("cannot connect to agent: " + err.Error()).AsError()
	}
	defer conn.Close()
	client := sshagent.NewClient(conn)
	passphrase, err := readPassphrase("Passphrase: ")
	if err != nil {
		return style.NewOutput().Error("read passphrase: " + err.Error()).AsError()
	}
	defer ClearBytes(passphrase)
	if err := client.Unlock(passphrase); err != nil {
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

func runUnlockKeys(socketPath string) error {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return style.NewOutput().Error("cannot connect to agent: " + err.Error()).AsError()
	}
	defer conn.Close()
	client := sshagent.NewClient(conn)
	passphrase, err := readPassphrase("Passphrase: ")
	if err != nil {
		return style.NewOutput().Error("read passphrase: " + err.Error()).AsError()
	}
	defer ClearBytes(passphrase)
	if err := client.Unlock(passphrase); err != nil {
		msg := err.Error()
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
