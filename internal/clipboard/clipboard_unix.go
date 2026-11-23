//go:build !windows

package clipboard

import (
	"fmt"
	"os"
	"runtime"
)

func copyText(text string) error {
	argv, err := defaultCommand(runtime.GOOS)
	if err != nil {
		return err
	}
	return runCommand(argv, text)
}

// defaultCommand is the command that copies on goos when $SSHUSH_COPY_CMD does
// not say otherwise.
func defaultCommand(goos string) ([]string, error) {
	switch goos {
	case "linux":
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			return []string{"wl-copy"}, nil
		}
		return []string{"xclip", "-selection", "clipboard"}, nil
	case "darwin":
		return []string{"pbcopy"}, nil
	default:
		return nil, fmt.Errorf("clipboard not supported on %s (set %s to a command that copies its input)", goos, CopyCmdEnv)
	}
}

func copySecret(text string) error {
	return copyText(text)
}
