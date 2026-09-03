package cli

import (
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/editcomment"
	"github.com/ollykeran/sshush/internal/keys"
	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
	ssh "golang.org/x/crypto/ssh"
)

func newEditCommand() *cobra.Command {
	var editorFlag string
	var commentFlag string
	var copyFlag bool
	var outputFlag string
	var filepathFlag string

	cmd := &cobra.Command{
		Use:   "edit <private-key-filepath | fingerprint | comment>",
		Short: "Edit comment on a private key",
		Long: "Edit an SSH private key comment, overwrite the key file or copy to a new file. " +
			"The argument can be a filepath, a SHA256 fingerprint, or a comment to look up from the running agent. " +
			"Pass --comment for a quick one-line edit; omit it to open $EDITOR instead.",
		Example: `sshush edit ~/.ssh/id_ed25519 --comment 'new-comment'  # fast: no editor
sshush edit ~/.ssh/id_rsa                              # opens $EDITOR
sshush edit SHA256:abc... --comment 'renamed'
sshush edit my-key-comment --comment 'updated'`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				cmd.Help()
				cmd.SilenceUsage = true
				return style.NewOutput().Error("exactly one key selector is required").AsError()
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEdit(args[0], editorFlag, commentFlag, cmd.Flags().Changed("comment"), copyFlag, outputFlag, filepathFlag)
		},
	}
	cmd.Flags().StringVarP(&editorFlag, "editor", "e", "", "editor command (default $EDITOR, fallback vim,nano,vi)")
	cmd.Flags().StringVarP(&commentFlag, "comment", "C", "", "new key comment (skip editor)")
	cmd.Flags().BoolVar(&copyFlag, "copy", false, "write edited key to a new file (requires -o/--output)")
	cmd.Flags().StringVarP(&outputFlag, "output", "o", "", "destination path when using --copy")
	cmd.Flags().StringVar(&filepathFlag, "filepath", "", "explicit source key file path (overrides auto-resolution)")
	return cmd
}

// resolveEditPath resolves the argument to a private key filepath.
// It tries: 1) explicit --filepath flag, 2) as a filepath, 3) as a fingerprint in the agent,
// 4) as a comment in the agent, 5) fallback to config KeyPaths.
func resolveEditPath(arg, filepathFlag string) (string, error) {
	// Explicit override takes highest priority
	if strings.TrimSpace(filepathFlag) != "" {
		path := utils.ExpandHomeDirectory(filepathFlag)
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("key file not found: %s", utils.DisplayPath(path))
		}
		return path, nil
	}

	// Try as filepath directly
	path := utils.ExpandHomeDirectory(arg)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	// Try as fingerprint or comment from the agent
	socketPath, err := getSocketPath()
	if session := openSessionIfRunning(err, socketPath); session != nil {
		defer session.Close()
		agentKeys, listErr := session.List()
		if listErr == nil {
			// Try fingerprint match
			for _, k := range agentKeys {
				pub, parseErr := ssh.ParsePublicKey(k.Blob)
				if parseErr != nil {
					continue
				}
				fp := ssh.FingerprintSHA256(pub)
				if fp == arg {
					if fp := agent.GetFilepath(fp); fp != "" {
						return fp, nil
					}
					// Fallback: check config KeyPaths
					if cfgPath := resolveFromConfig(fp); cfgPath != "" {
						return cfgPath, nil
					}
					return "", fmt.Errorf("fingerprint %s found in agent but source file path is unknown; use --filepath to specify", arg)
				}
			}
			// Try comment match
			for _, k := range agentKeys {
				if k.Comment == arg {
					pub, parseErr := ssh.ParsePublicKey(k.Blob)
					if parseErr != nil {
						continue
					}
					fp := ssh.FingerprintSHA256(pub)
					if fp := agent.GetFilepath(fp); fp != "" {
						return fp, nil
					}
					// Fallback: check config KeyPaths
					if cfgPath := resolveFromConfig(fp); cfgPath != "" {
						return cfgPath, nil
					}
					return "", fmt.Errorf("comment %q found in agent but source file path is unknown; use --filepath to specify", arg)
				}
			}
		}
	}

	// Last resort: check config KeyPaths by parsing each file
	if env.Config != nil {
		for _, cfgPath := range env.Config.KeyPaths {
			pub, _, _, parseErr := agent.ParseKeyFromPath(cfgPath)
			if parseErr != nil {
				continue
			}
			if ssh.FingerprintSHA256(pub) == arg || filepath.Base(cfgPath) == arg {
				return cfgPath, nil
			}
		}
	}

	return "", fmt.Errorf("key not found: %s", arg)
}

// resolveFromConfig tries to find a key file in the config KeyPaths by fingerprint.
func resolveFromConfig(fingerprint string) string {
	if env.Config == nil {
		return ""
	}
	for _, cfgPath := range env.Config.KeyPaths {
		pub, _, _, err := agent.ParseKeyFromPath(cfgPath)
		if err != nil {
			continue
		}
		if ssh.FingerprintSHA256(pub) == fingerprint {
			agent.RegisterFilepath(fingerprint, cfgPath)
			return cfgPath
		}
	}
	return ""
}

// openSessionIfRunning opens a Session, returning nil when the socket path is
// unusable or the agent is not reachable. Callers here treat an absent agent as
// "nothing to do" rather than as a failure.
func openSessionIfRunning(sockErr error, socketPath string) *agent.Session {
	if sockErr != nil {
		return nil
	}
	session, err := agent.Open(socketPath)
	if err != nil {
		return nil
	}
	return session
}

func runEdit(arg, editorFlag, commentFlag string, commentFlagSet bool, copyFlag bool, outputFlag, filepathFlag string) error {
	privateKeyPath, err := resolveEditPath(arg, filepathFlag)
	if err != nil {
		return style.NewOutput().Error(err.Error()).AsError()
	}

	if copyFlag && strings.TrimSpace(outputFlag) == "" {
		return style.NewOutput().Error("-o/--output is required when --copy is set").AsError()
	}
	if !copyFlag && strings.TrimSpace(outputFlag) != "" {
		return style.NewOutput().Error("-o/--output can only be used with --copy").AsError()
	}

	parsed, rawKey, signer, err := keys.LoadKeyMaterial(privateKeyPath)
	if err != nil {
		if strings.Contains(err.Error(), "encrypted keys not supported") {
			return style.NewOutput().Error("encrypted keys not supported").AsError()
		}
		return style.NewOutput().Error(err.Error()).AsError()
	}

	fingerprint := ssh.FingerprintSHA256(signer.PublicKey())

	comment := commentFlag
	if commentFlagSet {
		comment = strings.TrimSpace(comment)
		if comment == "" {
			return style.NewOutput().Error("comment cannot be empty").AsError()
		}
		if err := editcomment.Validate(comment); err != nil {
			return style.NewOutput().Error(err.Error()).AsError()
		}
	} else {
		comment, err = editcomment.EditCommentWithEditor(parsed.Comment, runtime.ResolveEditor(editorFlag))
		if err != nil {
			if errors.Is(err, editcomment.ErrExitedWithoutSaving) {
				style.NewOutput().Info(fmt.Sprintf("no changes made to %s", utils.DisplayPath(privateKeyPath))).Print()
				return nil
			}
			return style.NewOutput().Error(err.Error()).AsError()
		}
		comment = strings.TrimSpace(comment)
		if comment == "" {
			return style.NewOutput().Error("comment cannot be empty").AsError()
		}
	}

	printCommentDiff(parsed.Comment, comment).Print()

	destPath := privateKeyPath
	if copyFlag {
		destPath = utils.ExpandHomeDirectory(outputFlag)
	}

	if copyFlag {
		block, marshalErr := ssh.MarshalPrivateKey(rawKey, comment)
		if marshalErr != nil {
			return style.NewOutput().Error(fmt.Sprintf("marshal key: %v", marshalErr)).AsError()
		}
		if writeErr := os.WriteFile(destPath, pem.EncodeToMemory(block), 0o600); writeErr != nil {
			return style.NewOutput().Error(fmt.Sprintf("write private key: %v", writeErr)).AsError()
		}
		srcPubPath := privateKeyPath + ".pub"
		if _, statErr := os.Stat(srcPubPath); statErr == nil {
			if writeErr := keys.WritePub(rawKey, comment, destPath+".pub"); writeErr != nil {
				return style.NewOutput().Error(writeErr.Error()).AsError()
			}
		}
	} else {
		if err := keys.SaveWithComment(rawKey, comment, destPath); err != nil {
			return style.NewOutput().Error(err.Error()).AsError()
		}
	}

	out := style.NewOutput().
		Success("updated key comment").
		Info("fingerprint: " + fingerprint).
		Info("path: " + utils.DisplayPath(destPath))

	if copyFlag {
		out.Info("source: " + utils.DisplayPath(privateKeyPath))
	}

	// Reload the key in the agent if it is loaded, then persist the comment in
	// the vault when the agent uses the vault backend, so the on-disk key file
	// and the vault stay in sync. One session covers both.
	socketPath, sockErr := getSocketPath()
	if session := openSessionIfRunning(sockErr, socketPath); session != nil {
		defer session.Close()
		result := editcomment.SyncAgent(session, fingerprint, privateKeyPath, comment)
		if result.ReloadErr != nil {
			out.Warn("key updated on disk but agent reload failed: " + result.ReloadErr.Error())
		} else if result.Reloaded {
			out.Success("reloaded key in agent")
		}
		if result.VaultErr != nil {
			out.Warn("key file updated on disk but vault comment not updated: " + result.VaultErr.Error())
		} else if result.VaultSynced {
			out.Success("updated comment in vault")
		}
	}

	out.Print()
	return nil
}
