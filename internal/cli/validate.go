package cli

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"fmt"
	"os"
	"strings"

	"github.com/ollykeran/sshush/internal/keys"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
	ssh "golang.org/x/crypto/ssh"
)

func newValidateCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "validate <key-file>",
		Short:   "Validate and inspect a key file",
		Example: "sshush validate ~/.ssh/id_ed25519\nsshush validate ~/.ssh/id_ed25519.pub",
		Long:    "Validate an OpenSSH key file (private or public) and display its properties. When given a public key, looks for the matching private key in the same directory.",
		Args: nil,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				cmd.Usage()
				return style.NewOutput().Error("key file path is required").AsError()
			}
			return runValidate(args[0])
		},
	}
}

func runValidate(path string) error {
	path = utils.ExpandHomeDirectory(path)
	if _, err := os.Stat(path); err != nil {
		return style.NewOutput().Error(fmt.Sprintf("key file not found: %s", utils.DisplayPath(path))).AsError()
	}

	if strings.HasSuffix(path, ".pub") {
		return runValidatePub(path)
	}
	return runValidatePrivate(path)
}

func runValidatePub(pubPath string) error {
	pubData, err := os.ReadFile(pubPath)
	if err != nil {
		return style.NewOutput().Error(fmt.Sprintf("cannot read: %v", err)).AsError()
	}

	pub, _, _, _, err := ssh.ParseAuthorizedKey(pubData)
	if err != nil {
		return style.NewOutput().Error(fmt.Sprintf("invalid public key: %v", err)).AsError()
	}

	fp := ssh.FingerprintSHA256(pub)
	alg := pub.Type()
	bits, curve := keySizeInfo(pub)

	out := style.NewOutput()
	out.Add(style.Success("valid public key"))
	out.Add(style.Focus("type:        ") + style.Success(alg))
	out.Add(style.Focus("fingerprint: ") + style.Success(fp))
	if bits > 0 {
		out.Add(style.Focus("bits:        ") + style.Success(fmt.Sprintf("%d", bits)))
	}
	if curve != "" {
		out.Add(style.Focus("curve:       ") + style.Success(curve))
	}

	privPath := strings.TrimSuffix(pubPath, ".pub")
	if _, err := os.Stat(privPath); err == nil {
		privPub, _, signer, parseErr := keys.LoadKeyMaterial(privPath)
		if parseErr == nil {
			privPubKey := signer.PublicKey()
			privFP := ssh.FingerprintSHA256(privPubKey)
			if privFP == fp {
				out.Add(style.Focus("private:     ") + style.Success(fmt.Sprintf("%s (matches)", utils.DisplayPath(privPath))))
			} else {
				out.Add(style.Focus("private:     ") + style.Err(fmt.Sprintf("%s (fingerprint mismatch!)", utils.DisplayPath(privPath))))
			}
			if privPub.Comment != "" {
				out.Add(style.Focus("comment:     ") + style.Success(privPub.Comment))
			}
		} else {
			out.Add(style.Focus("private:     ") + style.Err(fmt.Sprintf("%s (unreadable: %v)", utils.DisplayPath(privPath), parseErr)))
		}
	} else {
		out.Add(style.Focus("private:     ") + style.Warn(fmt.Sprintf("%s (not found)", utils.DisplayPath(privPath))))
	}

	out.Print()
	return nil
}

func runValidatePrivate(path string) error {
	parsed, _, signer, err := keys.LoadKeyMaterial(path)
	if err != nil {
		if strings.Contains(err.Error(), "encrypted keys not supported") {
			return style.NewOutput().Error("encrypted keys not supported").AsError()
		}
		return style.NewOutput().Error(err.Error()).AsError()
	}

	pub := signer.PublicKey()
	fp := ssh.FingerprintSHA256(pub)
	alg := pub.Type()
	bits, curve := keySizeInfo(pub)

	out := style.NewOutput()
	out.Add(style.Success("valid key"))
	out.Add(style.Focus("type:        ") + style.Success(alg))
	out.Add(style.Focus("fingerprint: ") + style.Success(fp))
	if bits > 0 {
		out.Add(style.Focus("bits:        ") + style.Success(fmt.Sprintf("%d", bits)))
	}
	if curve != "" {
		out.Add(style.Focus("curve:       ") + style.Success(curve))
	}
	if parsed.Comment != "" {
		out.Add(style.Focus("comment:     ") + style.Success(parsed.Comment))
	}

	out.Add(style.Focus("public:      ") + style.Success(strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))))

	pubPath := path + ".pub"
	if pubData, err := os.ReadFile(pubPath); err == nil {
		pubLine := strings.TrimSpace(string(pubData))
		authorizedLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
		if strings.HasPrefix(pubLine, authorizedLine) || pubLine == authorizedLine {
			out.Add(style.Focus("pub file:    ") + style.Success(fmt.Sprintf("%s (matches)", utils.DisplayPath(pubPath))))
		} else {
			out.Add(style.Focus("pub file:    ") + style.Err(fmt.Sprintf("%s (mismatch!)", utils.DisplayPath(pubPath))))
		}
	}

	out.Print()
	return nil
}

func keySizeInfo(pub ssh.PublicKey) (bits int, curve string) {
	cp, ok := pub.(ssh.CryptoPublicKey)
	if !ok {
		return 0, ""
	}
	switch k := cp.CryptoPublicKey().(type) {
	case *rsa.PublicKey:
		return k.N.BitLen(), ""
	case *ecdsa.PublicKey:
		switch k.Curve.Params().BitSize {
		case 256:
			return 256, "nistp256"
		case 384:
			return 384, "nistp384"
		case 521:
			return 521, "nistp521"
		}
		return k.Curve.Params().BitSize, k.Curve.Params().Name
	case ed25519.PublicKey:
		return 256, ""
	}
	return 0, ""
}
