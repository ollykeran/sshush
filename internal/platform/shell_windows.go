package platform

import (
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fallbackShell is the shell assumed when neither the parent process nor $SHELL
// names one: PowerShell is on every Windows machine, a POSIX shell is not.
const fallbackShell = ShellPowerShell

// parentProcessName returns the image name of the process that ran sshush
// ("powershell.exe", "pwsh.exe", "cmd.exe"), or "".
func parentProcessName() string {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(snapshot)

	ppid := uint32(os.Getppid())
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if entry.ProcessID == ppid {
			return windows.UTF16ToString(entry.ExeFile[:])
		}
	}
	return ""
}

// powerShellProfilePath returns the current user's profile for the PowerShell
// that parentName names: PowerShell 7 (pwsh) keeps its own, and anything else
// gets Windows PowerShell's, the one every machine has. The Documents folder is
// asked for rather than assumed, since OneDrive commonly moves it — unless the
// environment points the home directory somewhere other than the account's own
// (as tests do), when the profile is looked for under that home instead.
func powerShellProfilePath(parentName string) (string, bool) {
	documents, err := windows.KnownFolderPath(windows.FOLDERID_Documents, 0)
	if err != nil || documents == "" {
		return "", false
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		account, err := windows.KnownFolderPath(windows.FOLDERID_Profile, 0)
		if err == nil && !strings.EqualFold(filepath.Clean(home), filepath.Clean(account)) {
			documents = filepath.Join(home, "Documents")
		}
	}
	dir := "WindowsPowerShell"
	if strings.HasPrefix(strings.ToLower(filepath.Base(parentName)), "pwsh") {
		dir = "PowerShell"
	}
	return filepath.Join(documents, dir, "Microsoft.PowerShell_profile.ps1"), true
}
