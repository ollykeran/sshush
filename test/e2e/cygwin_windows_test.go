package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// msysTool finds a program that comes with Git for Windows (an MSYS2 build,
// in <root>\usr\bin), or skips the test.
func msysTool(t *testing.T, name string) string {
	t.Helper()
	var dirs []string
	if git, err := exec.LookPath("git"); err == nil {
		for d := filepath.Dir(git); d != filepath.Dir(d); d = filepath.Dir(d) {
			dirs = append(dirs, filepath.Join(d, "usr", "bin"))
		}
	}
	dirs = append(dirs, filepath.Join(os.Getenv("ProgramFiles"), "Git", "usr", "bin"))
	for _, d := range dirs {
		if p := filepath.Join(d, name+".exe"); fileExists(p) {
			return p
		}
	}
	t.Skipf("Git for Windows' %s not found", name)
	return ""
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Git Bash's ssh-add reaches the running sshushd through the Cygwin socket
// file, at the path `sshush start` prints for a bash shell; the file is gone
// once the agent stops.
func TestE2E_GitBashSSHAddUsesTheAgentsKeys(t *testing.T) {
	sshAdd := msysTool(t, "ssh-add")
	dir := e2eWorkDir(t)
	binDir := buildBins(t)
	keyPath := writeE2ETestKey(t, dir, "id_ed25519", "e2e-cygwin-key")
	configPath := writeE2EConfig(t, dir, e2eAgentAddress(t, dir), "", []string{keyPath})
	t.Cleanup(func() { runSSHush(t, binDir, configPath, dir, nil, "stop") })
	stdout, stderr, code := runSSHush(t, binDir, configPath, dir, nil, "start", "--shell", "bash")
	if code != 0 {
		t.Fatalf("start: exit %d\nstderr: %s", code, stderr)
	}
	const prefix = "export SSH_AUTH_SOCK='"
	line := strings.TrimSpace(stdout)
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, "'") {
		t.Fatalf("start printed %q, want an export line", stdout)
	}
	sock := strings.TrimSuffix(strings.TrimPrefix(line, prefix), "'")
	if !strings.HasSuffix(sock, "/cygwin-agent.sock") {
		t.Fatalf("SSH_AUTH_SOCK=%s, want the Cygwin socket file", sock)
	}

	cmd := exec.Command(sshAdd, "-l")
	cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK="+sock)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "e2e-cygwin-key") {
		t.Fatalf("ssh-add -l: %v\n%s", err, out)
	}

	if _, stderr, code := runSSHush(t, binDir, configPath, dir, nil, "stop"); code != 0 {
		t.Fatalf("stop: exit %d\nstderr: %s", code, stderr)
	}
	if fileExists(filepath.FromSlash(sock)) {
		t.Errorf("socket file %s left behind after stop", sock)
	}
}
