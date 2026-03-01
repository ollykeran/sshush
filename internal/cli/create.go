package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ollykeran/sshush/internal/keys"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
	ssh "golang.org/x/crypto/ssh"
)

func newCreateCommand() *cobra.Command {
	var bits int
	var comment string
	var outputPath string
	var force bool

	cmd := &cobra.Command{
		Use:     "create <rsa|ecdsa|ed25519> <bits> -o <output-path>",
		Example: "sshush create rsa 2048 -o ~/.ssh/id_rsa",
		Short:   "Create a new SSH keypair",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(args[0], bits, comment, outputPath, force)
		},
	}

	cmd.Flags().IntVarP(&bits, "bits", "b", 4096, "key bits (rsa: 2048/3072/4096, ecdsa: 256/384/521)")
	cmd.Flags().StringVarP(&comment, "comment", "C", keys.DefaultComment(), "Comment for the key pair")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Private key output path (default ~/.ssh/id_<keytype>)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite output file, if it exists")

	return cmd
}

func runCreate(keyType string, bits int, comment, outputPath string, force bool) error {
	keyType = strings.ToLower(strings.TrimSpace(keyType))
	if keyType != "ed25519" && keyType != "rsa" && keyType != "ecdsa" {
		return style.NewOutput().Error("unsupported key type (use ed25519, rsa, or ecdsa)").AsError()
	}

	if outputPath == "" {
		outputPath = "~/.ssh/id_" + keyType
	}
	outputPath = utils.ExpandHomeDirectory(outputPath)

	if !force {
		if _, err := os.Stat(outputPath); err == nil {
			return style.NewOutput().
				Error("output file already exists").
				Info("re-run with --force to overwrite").
				AsError()
		}
	}

	privPEM, pubAuth, err := keys.Generate(keyType, bits, comment)
	if err != nil {
		return style.NewOutput().Error(err.Error()).AsError()
	}

	dir := filepath.Dir(outputPath)
	filename := filepath.Base(outputPath)
	if err := keys.SavePair(dir, filename, privPEM, pubAuth); err != nil {
		return style.NewOutput().Error(err.Error()).AsError()
	}

	fp := ""
	if raw, err := ssh.ParseRawPrivateKey(privPEM); err == nil {
		if signer, err := ssh.NewSignerFromKey(raw); err == nil {
			fp = ssh.FingerprintSHA256(signer.PublicKey())
		}
	}

	out := style.NewOutput().
		Success("created keypair").
		Info("private: " + outputPath).
		Info("public:  " + outputPath + ".pub").
		Info("type:    " + keyType)
	if fp != "" {
		out.Info(fmt.Sprintf("fingerprint: %s", fp))
	}
	out.Print()
	return nil
}
