package cli

import (
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"strings"

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

	cmd := &cobra.Command{
		Use:     "edit <private-key-filepath>",
		Long:    "Edit an SSH private key comment, overwrite the key file or copy to a new file.",
		Example: "sshush edit ~/.ssh/id_ed25519 --comment 'user@host' --copy --output ~/.ssh/id_ed25519.new",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				cmd.Help()
				cmd.SilenceUsage = true
				return style.NewOutput().Error("exactly one private key filepath is required").AsError()
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEdit(args[0], editorFlag, commentFlag, copyFlag, outputFlag)
		},
	}
	cmd.Flags().StringVarP(&editorFlag, "editor", "e", "", "editor command (default $EDITOR, fallback vi)")
	cmd.Flags().StringVarP(&commentFlag, "comment", "C", "", "new key comment (skip editor)")
	cmd.Flags().BoolVar(&copyFlag, "copy", false, "write edited key to a new file (requires --output)")
	cmd.Flags().StringVarP(&outputFlag, "output", "o", "", "destination path when using --copy")
	return cmd
}

func runEdit(privateKeyPath, editorFlag, commentFlag string, copyFlag bool, outputFlag string) error {
	privateKeyPath = utils.ExpandHomeDirectory(privateKeyPath)
	if _, err := os.Stat(privateKeyPath); err != nil {
		return style.NewOutput().Error(fmt.Sprintf("key file not found: %s", privateKeyPath)).AsError()
	}

	if copyFlag && strings.TrimSpace(outputFlag) == "" {
		return style.NewOutput().Error("--output is required when --copy is set").AsError()
	}
	if !copyFlag && strings.TrimSpace(outputFlag) != "" {
		return style.NewOutput().Error("--output can only be used with --copy").AsError()
	}

	parsed, rawKey, signer, err := keys.LoadKeyMaterial(privateKeyPath)
	if err != nil {
		if strings.Contains(err.Error(), "encrypted keys not supported") {
			return style.NewOutput().Error("encrypted keys not supported").AsError()
		}
		return style.NewOutput().Error(err.Error()).AsError()
	}

	comment := commentFlag
	if strings.TrimSpace(comment) == "" {
		comment, err = editCommentWithEditor(parsed.Comment, runtime.ResolveEditor(editorFlag))
		if err != nil {
			return style.NewOutput().Error(err.Error()).AsError()
		}
	}
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return style.NewOutput().Error("comment cannot be empty").AsError()
	}

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
		Info("path: " + destPath).
		Info("fingerprint: " + ssh.FingerprintSHA256(signer.PublicKey()))
	if copyFlag {
		out.Info("source: " + privateKeyPath)
	}
	out.Print()
	return nil
}

func resolveEditor(editorFlag string) string {
	return runtime.ResolveEditor(editorFlag)
}

func editCommentWithEditor(currentComment, editor string) (string, error) {
	tmp, err := os.CreateTemp("", "sshush-comment-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	defer tmp.Close()

	if _, err := tmp.WriteString(currentComment + "\n"); err != nil {
		return "", fmt.Errorf("write temp comment: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temp file: %w", err)
	}

	editorParts := strings.Fields(editor)
	if len(editorParts) == 0 {
		return "", fmt.Errorf("invalid editor command")
	}
	cmd := exec.Command(editorParts[0], append(editorParts[1:], tmpPath)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor failed: %w", err)
	}

	edited, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("read edited comment: %w", err)
	}
	return strings.TrimSpace(string(edited)), nil
}
