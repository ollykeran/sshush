// Package clipboard copies text to the system clipboard, for the CLI's and the
// TUI's "copy public key" and for the vault's recovery phrase.
package clipboard

// Copy writes text to the system clipboard: wl-copy under Wayland and xclip
// under X11 on Linux, pbcopy on macOS, the clipboard API on Windows. It returns
// an error if the platform is unsupported or the copy fails.
func Copy(text string) error {
	return copyText(text)
}

// CopySecret is Copy for text that should not outlive the paste it was copied
// for: a recovery phrase, not a public key. On Windows the clipboard is not
// only a clipboard: Win+V keeps a history of what passed through it, and cloud
// clipboard sends it to the user's other devices. CopySecret marks the text so
// that Windows does neither. The other platforms have no such history of their
// own to opt out of, so there it is the same as Copy.
func CopySecret(text string) error {
	return copySecret(text)
}
