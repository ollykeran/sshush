package cli

import (
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"

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

	cmd := &cobra.Command{
		Use:   "edit <private-key-filepath>",
		Short: "Edit comment on a private key file",
		Long:  "Edit an SSH private key comment, overwrite the key file or copy to a new file.",
		Example: `sshush edit ~/.ssh/id_ed25519 --comment 'new-comment'
sshush edit ~/.ssh/id_rsa`,
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
	cmd.Flags().StringVarP(&editorFlag, "editor", "e", "", "editor command (default $EDITOR, fallback vim,nano,vi)")
	cmd.Flags().StringVarP(&commentFlag, "comment", "C", "", "new key comment (skip editor)")
	cmd.Flags().BoolVar(&copyFlag, "copy", false, "write edited key to a new file (requires -o/--output)")
	cmd.Flags().StringVarP(&outputFlag, "output", "o", "", "destination path when using --copy")
	return cmd
}

func runEdit(privateKeyPath, editorFlag, commentFlag string, copyFlag bool, outputFlag string) error {
	privateKeyPath = utils.ExpandHomeDirectory(privateKeyPath)
	if _, err := os.Stat(privateKeyPath); err != nil {
		return style.NewOutput().Error(fmt.Sprintf("key file not found: %s", utils.DisplayPath(privateKeyPath))).AsError()
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
		Info("fingerprint: " + ssh.FingerprintSHA256(signer.PublicKey())).
		Info("path: " + utils.DisplayPath(destPath))

	if copyFlag {
		out.Info("source: " + utils.DisplayPath(privateKeyPath))
	}
	out.Print()
	return nil
}
