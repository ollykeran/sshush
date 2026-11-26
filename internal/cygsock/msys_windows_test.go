package cygsock

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// msysSSHAdd finds the ssh-add that comes with Git for Windows (an MSYS2
// program), or skips the test.
func msysSSHAdd(t *testing.T) string {
	t.Helper()
	var dirs []string
	if git, err := exec.LookPath("git"); err == nil {
		// git.exe is in <root>\cmd, <root>\bin or <root>\mingw64\bin; ssh-add
		// is in <root>\usr\bin.
		for d := filepath.Dir(git); d != filepath.Dir(d); d = filepath.Dir(d) {
			dirs = append(dirs, filepath.Join(d, "usr", "bin"))
		}
	}
	dirs = append(dirs, filepath.Join(os.Getenv("ProgramFiles"), "Git", "usr", "bin"))
	for _, d := range dirs {
		p := filepath.Join(d, "ssh-add.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("Git for Windows' ssh-add not found")
	return ""
}

// The real client: Git for Windows' ssh-add lists the agent's key through
// the socket file, named the way Windows names it.
func TestServe_gitForWindowsSSHAddListsTheKey(t *testing.T) {
	sshAdd := msysSSHAdd(t)
	path := serveKeyring(t, "msys-key")
	for _, sock := range []string{path, filepath.ToSlash(path)} {
		cmd := exec.Command(sshAdd, "-l")
		cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK="+sock)
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "msys-key") {
			t.Errorf("SSH_AUTH_SOCK=%s: ssh-add -l: %v\n%s", sock, err, out)
		}
	}
}
