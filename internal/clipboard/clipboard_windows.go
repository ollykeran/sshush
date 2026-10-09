package clipboard

import "github.com/atotto/clipboard"

// copyText goes through the clipboard API rather than piping to clip.exe, which
// reads its input in the console's code page and mangles anything not ASCII —
// a key comment with an accent in it, say.
func copyText(text string) error {
	return clipboard.WriteAll(text)
}
