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
	sshagent "golang.org/x/crypto/ssh/agent"
)

func newAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "add <key_paths...>",
		Example: "sshush add ~/.ssh/id_ed25519 ~/.ssh/id_rsa",
		Short:   "Add key(s) to the running agent",
		Long:    "Add unencrypted OpenSSH private key(s) to sshushd by filepath",
		RunE:    runAdd,
	}
	cmd.Flags().Bool("auto", false, "set autoload so the key is loaded when the daemon starts")
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
	before, err := agent.ListKeysFromSocket(socketPath)
	if err != nil {
		return style.NewOutput().Error("failed to list keys from socket").AsError()
	}
	auto, _ := cmd.Flags().GetBool("auto")
	out := style.NewOutput()
	for _, arg := range paths {
		path := utils.ExpandHomeDirectory(arg)
		if auto {
			if _, err := os.Stat(path); err != nil {
				resolved, resolveErr := resolveKeyPathByComment(arg, env.Config)
				if resolveErr != nil {
					return resolveErr
				}
				path = utils.ExpandHomeDirectory(resolved)
			}
			payload, err := vault.BuildAddKeyOptsPayload(path, true)
			if err != nil {
				return style.NewOutput().Error("failed to read key: " + err.Error()).AsError()
			}
			_, err = agent.CallExtension(socketPath, vault.ExtensionAddKeyOpts, payload)
			if err != nil {
				if errors.Is(err, sshagent.ErrExtensionUnsupported) {
					if err2 := agent.AddKeyToSocketFromPath(socketPath, path); err2 != nil {
						if errors.Is(err2, openssh.ErrEncryptedPrivateKey) {
							return style.NewOutput().Error(err2.Error()).AsError()
						}
						return style.NewOutput().Error("failed to add key to socket").AsError()
					}
					out.Warn("--auto has no effect (agent is not a vault)")
				} else {
					msg := err.Error()
					if msg == "agent: generic extension failure" && env.Config != nil && env.Config.VaultPath != "" {
						msg = "vault is locked; unlock first with 'sshush start' (enter passphrase) or 'sshush vault unlock-recovery'"
					} else {
						msg = "failed to add key: " + msg
					}
					return style.NewOutput().Error(msg).AsError()
				}
			}
			continue
		}
		if err := agent.AddKeyToSocketFromPath(socketPath, path); err == nil {
			continue
		} else if errors.Is(err, openssh.ErrEncryptedPrivateKey) {
			return style.NewOutput().Error(err.Error()).AsError()
		}
		resolved, resolveErr := resolveKeyPathByComment(arg, env.Config)
		if resolveErr != nil {
			return style.NewOutput().Error("failed to resolve key path by comment").AsError()
		}
		resPath := utils.ExpandHomeDirectory(resolved)
		if err := agent.AddKeyToSocketFromPath(socketPath, resPath); err != nil {
			if errors.Is(err, openssh.ErrEncryptedPrivateKey) {
				return style.NewOutput().Error(err.Error()).AsError()
			}
			return style.NewOutput().Error("failed to add key to socket").AsError()
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
