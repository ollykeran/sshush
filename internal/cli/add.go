package cli

import (
	"errors"
	"fmt"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/openssh"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
)

func newAddCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "add <key_paths...>",
		Example: "sshush add ~/.ssh/id_ed25519 ~/.ssh/id_rsa",
		Short:   "Add key(s) to the running agent",
		Long:    "Add unencrypted OpenSSH private key(s) to sshushd by filepath",
		RunE:    runAdd,
	}
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
	before, err := agent.ListKeysFromSocket(socketPath)
	if err != nil {
		return style.NewOutput().Error("failed to list keys from socket").AsError()
	}
	for _, arg := range paths {
		keyPath := utils.ExpandHomeDirectory(arg)
		if err := agent.AddKeyToSocketFromPath(socketPath, keyPath); err == nil {
			continue
		} else if errors.Is(err, openssh.ErrEncryptedPrivateKey) {
			return style.NewOutput().Error(err.Error()).AsError()
		}
		resolved, resolveErr := resolveKeyPathByComment(arg, env.Config)
		if resolveErr != nil {
			return style.NewOutput().Error("failed to resolve key path by comment").AsError()
		}
		if err := agent.AddKeyToSocketFromPath(socketPath, resolved); err != nil {
			if errors.Is(err, openssh.ErrEncryptedPrivateKey) {
				return style.NewOutput().Error(err.Error()).AsError()
			}
			return style.NewOutput().Error("failed to add key to socket").AsError()
		}
	}
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
