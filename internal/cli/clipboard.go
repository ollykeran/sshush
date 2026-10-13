package cli

import "github.com/ollykeran/sshush/internal/clipboard"

// CopyToClipboard writes text to the system clipboard (see package clipboard).
func CopyToClipboard(text string) error {
	return clipboard.Copy(text)
}

// CopySecretToClipboard is CopyToClipboard for something that must not linger:
// on Windows it is kept out of clipboard history and cloud sync.
func CopySecretToClipboard(text string) error {
	return clipboard.CopySecret(text)
}
