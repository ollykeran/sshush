package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// CopyToClipboard writes text to the system clipboard. Uses wl-copy (Wayland),
// xclip (X11), or pbcopy (macOS). Returns an error if the platform is unsupported
// or the command fails.
func CopyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			cmd = exec.Command("wl-copy")
		} else {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		}
	case "darwin":
		cmd = exec.Command("pbcopy")
	default:
		return fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
