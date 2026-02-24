package cli

import (
	"fmt"
	"net"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/spf13/cobra"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func newAddCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "add [key-path|comment...]",
		Short: "Add key(s) to the running agent by file path or comment",
		RunE:  runAdd,
	}
}

func runAdd(cmd *cobra.Command, args []string) error {
	if env.Config == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}
	paths := args
	if len(paths) == 0 {
		paths = env.Config.KeyPaths
	}
	if len(paths) == 0 {
		cmd.Usage()
		return nil
	}
	socketPath, err := getSocketPath()
	if err != nil {
		return err
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	client := sshagent.NewClient(conn)
	before, _ := client.List()
	for _, arg := range paths {
		if err := agent.AddKeyFromPath(client, arg); err == nil {
			continue
		}
		resolved, resolveErr := resolveKeyPathByComment(arg)
		if resolveErr != nil {
			return resolveErr
		}
		if err := agent.AddKeyFromPath(client, resolved); err != nil {
			return err
		}
	}
	after, _ := client.List()
	printKeysDiff(agentKeysToEntries(before), agentKeysToEntries(after)).Print()
	return nil
}

// resolveKeyPathByComment searches configured key paths for a key whose comment
// matches the given argument.
func resolveKeyPathByComment(comment string) (string, error) {
	if env.Config == nil {
		return "", style.NewOutput().Error(fmt.Sprintf("no key file matches %q", comment)).AsError()
	}
	for _, path := range env.Config.KeyPaths {
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
