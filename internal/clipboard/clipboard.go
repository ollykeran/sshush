// Package clipboard copies text to the system clipboard, for the CLI's and the
// TUI's "copy public key".
package clipboard

// Copy writes text to the system clipboard: wl-copy under Wayland and xclip
// under X11 on Linux, pbcopy on macOS, the clipboard API on Windows. It returns
// an error if the platform is unsupported or the copy fails.
func Copy(text string) error {
	return copyText(text)
}
