package cli

import "github.com/ollykeran/sshush/internal/clipboard"

// CopyToClipboard writes text to the system clipboard (see package clipboard).
func CopyToClipboard(text string) error {
	return clipboard.Copy(text)
}
