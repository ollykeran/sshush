package cli

import (
	"errors"
	"fmt"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/openssh"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
	ssh "golang.org/x/crypto/ssh"
)

func newRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "remove [key-path|comment...]",
		Short:   "Remove key(s) from the running agent",
		Example: "sshush remove ~/.ssh/id_ed25519 ~/.ssh/id_rsa",
		Long:    "Remove key(s) from the running agent by file path or comment.",
		RunE:    runRemove,
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
	before, err := agent.ListKeysFromSocket(socketPath)
	if err != nil {
		return err
	}
	wantFP := make(map[string]bool)
	for _, arg := range args {
		keyPath := utils.ExpandHomeDirectory(arg)
		if pubKey, _, _, err := agent.ParseKeyFromPath(keyPath); err == nil {
			wantFP[ssh.FingerprintSHA256(pubKey)] = true
			continue
		} else if errors.Is(err, openssh.ErrEncryptedPrivateKey) {
			return style.NewOutput().Error(err.Error()).AsError()
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
			if _, err := agent.RemoveKeyFromSocketByFingerprint(socketPath, ssh.FingerprintSHA256(key)); err != nil {
				return style.NewOutput().Error(fmt.Sprintf("remove %s: %v", key.Comment, err)).AsError()
			}
		}
	}
	after, _ := agent.ListKeysFromSocket(socketPath)
	printKeysDiff(agentKeysToEntries(before), agentKeysToEntries(after)).Print()
	return nil
}
