package keys

import (
	"strings"

	ssh "golang.org/x/crypto/ssh"
)

// FormatPublicKey returns an authorized_keys-formatted line with comment.
func FormatPublicKey(signer ssh.Signer, comment string) string {
	pubLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return pubLine + "\n"
	}
	return pubLine + " " + comment + "\n"
}
