package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/ollykeran/sshush/internal/keys"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
	ssh "golang.org/x/crypto/ssh"
)

func newExportCommand() *cobra.Command {
	var outputPath string

	cmd := &cobra.Command{
		Use:     "export <private-key-filepath>",
		Short:   "Export public key from a private key",
		Example: "sshush export ~/.ssh/id_ed25519 --output ~/.ssh/id_ed25519.pub",
		Long:    "Export an OpenSSH public key from an unencrypted OpenSSH private key file",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExport(args[0], outputPath)
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "write public key to file (default: stdout, default extension: .pub)")

	return cmd
}

func runExport(privateKeyPath, outputPath string) error {
	privateKeyPath = utils.ExpandHomeDirectory(privateKeyPath)
	if _, err := os.Stat(privateKeyPath); err != nil {
		return style.NewOutput().Error(fmt.Sprintf("key file not found: %s", privateKeyPath)).AsError()
	}
	parsed, _, signer, err := keys.LoadKeyMaterial(privateKeyPath)
	if err != nil {
		if strings.Contains(err.Error(), "encrypted keys not supported") {
			return style.NewOutput().Error("encrypted keys not supported").AsError()
		}
		return style.NewOutput().Error(err.Error()).AsError()
	}

	pubKey := keys.FormatPublicKey(signer, parsed.Comment)

	if strings.TrimSpace(outputPath) == "" {
		fmt.Fprint(os.Stdout, pubKey)
		return nil
	}

	outputPath = utils.ExpandHomeDirectory(outputPath)
	if err := os.WriteFile(outputPath, []byte(pubKey), 0o644); err != nil {
		return style.NewOutput().Error(fmt.Sprintf("write public key: %v", err)).AsError()
	}

	style.NewOutput().
		Success("exported public key").
		Info("output: " + outputPath).
		Info("fingerprint: " + ssh.FingerprintSHA256(signer.PublicKey())).
		Print()
	return nil
}
