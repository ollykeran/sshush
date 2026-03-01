package cli

import (
	"fmt"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/spf13/cobra"
)

func newAddCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "add <key_paths...>",
		Example: "sshush add ~/.ssh/id_ed25519 ~/.ssh/id_rsa",
		Short:   "Add key(s) to the running agent",
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
		return nil
	}
	socketPath, err := getSocketPath()
	if err != nil {
		return err
	}
	if !sshushd.CheckAlreadyRunning(socketPath) {
		return style.NewOutput().Error("Agent not running. Please start the agent with 'sshush start'").AsError()
	}
	before, err := agent.ListKeysFromSocket(socketPath)
	if err != nil {
		return err
	}
	for _, arg := range paths {
		if err := agent.AddKeyToSocketFromPath(socketPath, arg); err == nil {
			continue
		}
		resolved, resolveErr := resolveKeyPathByComment(arg, env.Config)
		if resolveErr != nil {
			return resolveErr
		}
		if err := agent.AddKeyToSocketFromPath(socketPath, resolved); err != nil {
			return err
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
