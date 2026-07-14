package cli

import (
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/editcomment"
	"github.com/ollykeran/sshush/internal/keys"
	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
	"github.com/spf13/cobra"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
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
			"The argument can be a filepath, a SHA256 fingerprint, or a comment to look up from the running agent.",
		Example: `sshush edit ~/.ssh/id_ed25519 --comment 'new-comment'
sshush edit ~/.ssh/id_rsa
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
			return runEdit(args[0], editorFlag, commentFlag, copyFlag, outputFlag, filepathFlag)
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
	if err == nil && sshushd.CheckAlreadyRunning(socketPath) {
		agentKeys, listErr := agent.ListKeysFromSocket(socketPath)
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

// isKeyLoadedInAgent checks if a key with the given fingerprint is loaded in the running agent.
func isKeyLoadedInAgent(socketPath, fingerprint string) bool {
	if !sshushd.CheckAlreadyRunning(socketPath) {
		return false
	}
	agentKeys, err := agent.ListKeysFromSocket(socketPath)
	if err != nil {
		return false
	}
	for _, k := range agentKeys {
		pub, err := ssh.ParsePublicKey(k.Blob)
		if err != nil {
			continue
		}
		if ssh.FingerprintSHA256(pub) == fingerprint {
			return true
		}
	}
	return false
}

// reloadKeyInAgent removes the old key and re-adds it after an edit.
// For vault mode, uses add-key-opts; for standard mode, removes and re-adds.
func reloadKeyInAgent(socketPath, privateKeyPath, newComment string) error {
	if !sshushd.CheckAlreadyRunning(socketPath) {
		return nil
	}
	mode, live := agent.LiveBackendMode(socketPath)
	if !live {
		return nil
	}

	// Find and remove the old key
	agentKeys, err := agent.ListKeysFromSocket(socketPath)
	if err != nil {
		return fmt.Errorf("list keys: %w", err)
	}
	conn, dialErr := net.Dial("unix", socketPath)
	if dialErr != nil {
		return fmt.Errorf("connect to agent: %w", dialErr)
	}
	defer conn.Close()
	client := sshagent.NewClient(conn)

	for _, k := range agentKeys {
		pub, parseErr := ssh.ParsePublicKey(k.Blob)
		if parseErr != nil {
			continue
		}
		fp := ssh.FingerprintSHA256(pub)
		existingFP := ""
		if _, statErr := os.Stat(privateKeyPath); statErr == nil {
			pubKey, _, _, err := agent.ParseKeyFromPath(privateKeyPath)
			if err == nil {
				existingFP = ssh.FingerprintSHA256(pubKey)
			}
		}
		if existingFP != "" && fp == existingFP {
			_ = client.Remove(pub)
			break
		}
	}

	// Re-add the key with updated comment
	if mode == "vault" {
		if err := vault.AddPrivateKeyFileToSocket(socketPath, privateKeyPath, true); err != nil {
			return fmt.Errorf("reload key in vault: %w", err)
		}
	} else {
		if err := agent.AddKeyToSocketFromPath(socketPath, privateKeyPath); err != nil {
			return fmt.Errorf("reload key in agent: %w", err)
		}
	}
	return nil
}

func runEdit(arg, editorFlag, commentFlag string, copyFlag bool, outputFlag, filepathFlag string) error {
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
	if strings.TrimSpace(comment) == "" {
		comment, err = editcomment.EditCommentWithEditor(parsed.Comment, runtime.ResolveEditor(editorFlag))
		if err != nil {
			if errors.Is(err, editcomment.ErrExitedWithoutSaving) {
				style.NewOutput().Info(fmt.Sprintf("no changes made to %s", utils.DisplayPath(privateKeyPath))).Print()
				return nil
			}
			return style.NewOutput().Error(err.Error()).AsError()
		}
	}
	comment = strings.TrimSpace(comment)

	if comment == "" {
		return style.NewOutput().Error("comment cannot be empty").AsError()
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
			if writeErr := os.WriteFile(destPath+".pub", []byte(keys.FormatPublicKey(signer, comment)), 0o644); writeErr != nil {
				return style.NewOutput().Error(fmt.Sprintf("write public key: %v", writeErr)).AsError()
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

	// Reload key in agent if it's loaded
	socketPath, sockErr := getSocketPath()
	if sockErr == nil && isKeyLoadedInAgent(socketPath, fingerprint) {
		if reloadErr := reloadKeyInAgent(socketPath, privateKeyPath, comment); reloadErr != nil {
			out.Warn("key updated on disk but agent reload failed: " + reloadErr.Error())
		} else {
			out.Success("reloaded key in agent")
		}
	}

	out.Print()
	return nil
}
