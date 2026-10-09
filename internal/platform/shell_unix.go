//go:build !windows

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// fallbackShell is the shell assumed when neither the parent process nor $SHELL names one.
const fallbackShell = ShellPosix

// parentProcessName returns the name of the process that ran sshush, or "".
func parentProcessName() string {
	ppid := strconv.Itoa(os.Getppid())
	if data, err := os.ReadFile(filepath.Join("/proc", ppid, "comm")); err == nil {
		return strings.TrimSpace(string(data))
	}
	out, err := exec.Command("ps", "-o", "comm=", "-p", ppid).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// powerShellProfilePath has no answer off Windows.
func powerShellProfilePath(string) (string, bool) { return "", false }
