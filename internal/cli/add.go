package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/openssh"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
	"github.com/spf13/cobra"
)

func newAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "add <key_paths...>",
		Example: "sshush add ~/.ssh/id_ed25519 ~/.ssh/id_rsa",
		Short:   "Add key(s) to the running agent",
		Long: "Add unencrypted OpenSSH private key(s) to sshushd by filepath. " +
			"When the agent is a vault, keys are stored with autoload on by default so they load after daemon restart; " +
			"use --no-autoload to keep the key only for this session.",
		RunE: runAdd,
	}
	cmd.Flags().Bool("no-autoload", false, "when the agent is a vault, add the key without autoload (session-only until restart)")
	return cmd
}

func runAdd(cmd *cobra.Command, args []string) error {
	if env.Config == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}
	paths := args
	if len(paths) == 0 {
		cmd.Usage()
		return style.NewOutput().Error("at least one key path is required").AsError()
	}
	socketPath, err := getSocketPath()
	if err != nil {
		return style.NewOutput().Error("failed to get socket path").AsError()
	}
	if !sshushd.CheckAlreadyRunning(socketPath) {
		return style.NewOutput().Error("Agent not running. Please start the agent with 'sshush start'").AsError()
	}
	noAutoload, _ := cmd.Flags().GetBool("no-autoload")
	autoload := !noAutoload

	mode, live := agent.LiveBackendMode(socketPath)
	before, err := agent.ListKeysFromSocket(socketPath)
	if err != nil {
		return style.NewOutput().Error("failed to list keys from socket").AsError()
	}
	out := style.NewOutput()
	for _, arg := range paths {
		path := utils.ExpandHomeDirectory(arg)
		if _, err := os.Stat(path); err != nil {
			resolved, resolveErr := resolveKeyPathByComment(arg, env.Config)
			if resolveErr != nil {
				return resolveErr
			}
			path = utils.ExpandHomeDirectory(resolved)
		}
		if live && mode == "vault" {
			if err := vault.AddPrivateKeyFileToSocket(socketPath, path, autoload); err != nil {
				msg := err.Error()
				if msg == "agent: generic extension failure" && env.Config != nil && env.Config.AgentVault && env.Config.VaultPath != "" {
					msg = "vault is locked; unlock first with 'sshush start' (enter passphrase) or 'sshush vault unlock-recovery'"
				} else {
					msg = "failed to add key: " + msg
				}
				return style.NewOutput().Error(msg).AsError()
			}
			continue
		}
		if err := agent.AddKeyToSocketFromPath(socketPath, path); err != nil {
			if errors.Is(err, openssh.ErrEncryptedPrivateKey) {
				return style.NewOutput().Error(err.Error()).AsError()
			}
			return style.NewOutput().Error("failed to add key to socket").AsError()
		}
		if noAutoload {
			out.Warn("--no-autoload has no effect (agent is not a vault)")
		}
	}
	out.PrintErr()
	after, _ := agent.ListKeysFromSocket(socketPath)
	printKeysDiff(agentKeysToEntries(before), agentKeysToEntries(after)).Print()
	return nil
}

func resolveKeyPathByComment(comment string, cfg *config.Config) (string, error) {
	if cfg == nil {
		return "", style.NewOutput().Error(fmt.Sprintf("no key file matches %q", comment)).AsError()
	}
	for _, path := range cfg.KeyPaths {
		_, c, _, err := agent.ParseKeyFromPath(path)
		if err != nil {
			continue
		}
		if c == comment {
			return path, nil
		}
	}
	return "", style.NewOutput().Error(fmt.Sprintf("no configured key matches %q", comment)).AsError()
}
