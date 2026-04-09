package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ollykeran/sshush/internal/keys"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
	ssh "golang.org/x/crypto/ssh"
)

const (
	rsaBitsUsageHint   = "use 2048, 3072, or 4096 (default 4096; set with --bits / -b)"
	ecdsaBitsUsageHint = "use 256 (nistp256), 384 (nistp384), or 521 (nistp521); default is 256 if --bits / -b is omitted"
)

func newCreateCommand() *cobra.Command {
	var bits int
	var comment string
	var outputPath string
	var force bool

	cmd := &cobra.Command{
		Use:     "create <rsa|ecdsa|ed25519> [bits] -o <output-path>",
		Example: "sshush create rsa 2048 -o ~/.ssh/id_rsa",
		Short:   "Create a new SSH keypair",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := effectiveBitsForCreate(cmd, args, bits)
			if err != nil {
				return err
			}
			return runCreate(args[0], b, comment, outputPath, force)
		},
	}

	cmd.Flags().IntVarP(&bits, "bits", "b", 4096, "key bits (rsa: 2048/3072/4096, ecdsa: 256/384/521); optional 2nd positional sets this unless -b is used")
	cmd.Flags().StringVarP(&comment, "comment", "C", keys.DefaultComment(), "Comment for the key pair")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Private key output path (default ~/.ssh/id_<keytype>)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite output file, if it exists")

	return cmd
}

// effectiveBitsForCreate applies optional positional [bits] when present. If --bits / -b was
// explicitly set, the flag wins over the second argument. ed25519 must not take a bits argument.
// For ecdsa with one argument and no explicit -b, bits 0 means P256 (the shared flag default is for rsa).
func effectiveBitsForCreate(cmd *cobra.Command, args []string, flagBits int) (int, error) {
	if len(args) < 2 {
		kt := strings.ToLower(strings.TrimSpace(args[0]))
		if kt == "ecdsa" && !cmd.Flags().Changed("bits") {
			return 0, nil
		}
		return flagBits, nil
	}
	keyType := strings.ToLower(strings.TrimSpace(args[0]))
	switch keyType {
	case "ed25519":
		return 0, style.NewOutput().
			Error("ed25519 has no key size; remove the second argument").
			Info("only rsa and ecdsa use a bits value (positional or --bits / -b)").
			AsError()
	case "rsa", "ecdsa":
		parsed, err := strconv.Atoi(strings.TrimSpace(args[1]))
		if err != nil {
			return 0, style.NewOutput().
				Error(fmt.Sprintf("invalid bits value %q", args[1])).
				Info("use an integer (e.g. 2048 for rsa, 256 for ecdsa)").
				AsError()
		}
		if cmd.Flags().Changed("bits") {
			return flagBits, nil
		}
		return parsed, nil
	default:
		return flagBits, nil
	}
}

func runCreate(keyType string, bits int, comment, outputPath string, force bool) error {
	keyType = strings.ToLower(strings.TrimSpace(keyType))
	if keyType != "ed25519" && keyType != "rsa" && keyType != "ecdsa" {
		return style.NewOutput().Error("unsupported key type (use ed25519, rsa, or ecdsa)").AsError()
	}

	if keyType == "rsa" {
		switch bits {
		case 0, 2048, 3072, 4096:
		default:
			return style.NewOutput().
				Error(fmt.Sprintf("unsupported rsa key size: %d", bits)).
				Info(rsaBitsUsageHint).
				AsError()
		}
	}

	if keyType == "ecdsa" {
		switch bits {
		case 0, 256, 384, 521:
		default:
			return style.NewOutput().
				Error(fmt.Sprintf("unsupported ecdsa curve size: %d", bits)).
				Info(ecdsaBitsUsageHint).
				AsError()
		}
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
		out := style.NewOutput().Error(err.Error())
		switch {
		case keyType == "rsa" && strings.Contains(err.Error(), "unsupported rsa key size"):
			out.Info(rsaBitsUsageHint)
		case keyType == "ecdsa" && strings.Contains(err.Error(), "unsupported ecdsa curve size"):
			out.Info(ecdsaBitsUsageHint)
		}
		return out.AsError()
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
		Info("private: " + utils.DisplayPath(outputPath)).
		Info("public:  " + utils.DisplayPath(outputPath+".pub")).
		Info("type:    " + keyType)
	if fp != "" {
		out.Info(fmt.Sprintf("fingerprint: %s", fp))
	}
	out.Print()
	return nil
}
