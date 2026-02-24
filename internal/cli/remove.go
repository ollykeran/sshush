package cli

import (
	"fmt"
	"net"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/spf13/cobra"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func newRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove [key-path|comment...]",
		Short: "Remove key(s) from the running agent by file path or comment",
		RunE:  runRemove,
	}
}

func runRemove(cmd *cobra.Command, args []string) error {
	if env.Config == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}
	if len(args) == 0 {
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
	before, err := client.List()
	if err != nil {
		return err
	}
	wantFP := make(map[string]bool)
	for _, arg := range args {
		if pubKey, _, _, err := agent.ParseKeyFromPath(arg); err == nil {
			wantFP[ssh.FingerprintSHA256(pubKey)] = true
			continue
		}
		matched := false
		for _, key := range before {
			fp := ssh.FingerprintSHA256(key)
			if key.Comment == arg || fp == arg {
				wantFP[fp] = true
				matched = true
			}
		}
		if !matched {
			return style.NewOutput().Error(fmt.Sprintf("no loaded key matches %q", arg)).AsError()
		}
	}
	for _, key := range before {
		if wantFP[ssh.FingerprintSHA256(key)] {
			if err := client.Remove(key); err != nil {
				return style.NewOutput().Error(fmt.Sprintf("remove %s: %v", key.Comment, err)).AsError()
			}
		}
	}
	after, _ := client.List()
	printKeysDiff(agentKeysToEntries(before), agentKeysToEntries(after)).Print()
	return nil
}
