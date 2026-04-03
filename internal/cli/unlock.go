package cli

import (
	"errors"
	"net"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/vault"
	"github.com/spf13/cobra"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func newUnlockCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "unlock",
		Short: "Unlock the vault with passphrase",
		Long:  "Connect to the running agent and unlock the vault using the master passphrase. Only applies when [agent].vault = true and [vault].vault_path is set.",
		Args:  cobra.NoArgs,
		RunE:  runUnlock,
	}
}

func runUnlock(cmd *cobra.Command, _ []string) error {
	if env.Config == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}
	if !env.Config.AgentVault || env.Config.VaultPath == "" {
		return style.NewOutput().
			Error("unlock only applies when the agent uses a vault; set [agent].vault = true and [vault].vault_path, then run 'sshush start'").
			AsError()
	}
	socketPath := env.Config.SocketPath
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
	if err := client.Unlock(passphrase); err != nil {
		for i := range passphrase {
			passphrase[i] = 0
		}
		msg := err.Error()
		if msg == "agent: failure" {
			msg = "unlock failed: wrong passphrase, or the running agent is not a vault (run 'sshush start' after setting [vault].vault_path in config)"
		} else {
			msg = "unlock failed: " + msg
		}
		return style.NewOutput().Error(msg).AsError()
	}
	for i := range passphrase {
		passphrase[i] = 0
	}
	style.NewOutput().Success("Vault unlocked.").PrintErr()
	return nil
}
