//go:build linux

package e2e

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sshagent "golang.org/x/crypto/ssh/agent"
)

// These tests cross the real WSL boundary: an sshush in the distro, with
// [agent].type = "windows", against an sshush.exe and sshushd.exe on the
// Windows side. They only mean anything inside WSL, so they skip unless asked
// for:
//
//	SSHUSH_E2E_WSL=1
//	SSHUSH_E2E_WINDOWS_BIN_DIR=/mnt/c/.../build/windows-amd64
//
// "just e2e-wsl" sets both. Once asked for, a missing piece is a failure
// rather than a skip, so a CI job cannot pass by testing nothing.
const (
	wslEnv           = "SSHUSH_E2E_WSL"
	windowsBinDirEnv = "SSHUSH_E2E_WINDOWS_BIN_DIR"
)

// wslRig is one test's pair of installs: the Linux sshush in the distro and
// the Windows one it relays to, each with a config of its own.
type wslRig struct {
	t *testing.T
	// binDir holds the Linux sshush and sshushd; winBinDir the Windows ones, as
	// the distro sees them (/mnt/c/...).
	binDir, winBinDir string
	// dir is the distro's side: config, socket, pidfile. winDir is the Windows
	// side's, on a Windows drive so both can read it.
	dir, winDir string
	configPath  string
	socketPath  string
}

type wslRigOptions struct {
	// windowsKeyComment, when set, puts a key with that comment in the Windows
	// agent's key_paths. A config for a keys agent is not valid without one.
	windowsKeyComment string
	// vault makes the Windows agent a vault. The vault is initialised with
	// e2ePassphrase and left locked.
	vault bool
	// findOnPath leaves [agent].windows_sshush out of the distro's config and
	// puts the Windows binaries on PATH instead.
	findOnPath bool
}

func newWSLRig(t *testing.T, opts wslRigOptions) *wslRig {
	t.Helper()
	if os.Getenv(wslEnv) == "" {
		t.Skipf("set %s=1 inside WSL to run the Windows interop tests", wslEnv)
	}
	if interop, _ := filepath.Glob("/proc/sys/fs/binfmt_misc/WSLInterop*"); len(interop) == 0 {
		t.Fatalf("%s is set but this is not WSL, or Windows interop is turned off", wslEnv)
	}
	winBinDir := os.Getenv(windowsBinDirEnv)
	if winBinDir == "" {
		t.Fatalf("%s must name the directory holding sshush.exe and sshushd.exe, as WSL sees it", windowsBinDirEnv)
	}
	for _, name := range []string{"sshush.exe", "sshushd.exe"} {
		if _, err := os.Stat(filepath.Join(winBinDir, name)); err != nil {
			t.Fatalf("%s: %v", windowsBinDirEnv, err)
		}
	}
	r := &wslRig{t: t, binDir: buildBins(t), winBinDir: winBinDir, dir: e2eWorkDir(t)}
	r.socketPath = filepath.Join(r.dir, "agent.sock")

	winDir, err := os.MkdirTemp(winBinDir, "e2e-")
	if err != nil {
		t.Fatalf("create the Windows side's directory: %v", err)
	}
	r.winDir = winDir
	t.Cleanup(func() { _ = os.RemoveAll(winDir) })

	// The bridge runs sshush.exe with no --config, so the Windows side finds its
	// config the default way. XDG_CONFIG_HOME, carried across by WSLENV with its
	// path translated, points that at this test's directory: the Windows daemon
	// gets a config, a pidfile and a pipe of its own, and the agent the
	// developer is actually using is never touched. SSHUSH_CONFIG is not
	// carried across; it names the distro's config.
	t.Setenv("XDG_CONFIG_HOME", winDir)
	wslenv := "XDG_CONFIG_HOME/p"
	if existing := os.Getenv("WSLENV"); existing != "" {
		wslenv = existing + ":" + wslenv
	}
	t.Setenv("WSLENV", wslenv)

	var win strings.Builder
	win.WriteString("[agent]\n")
	fmt.Fprintf(&win, "socket_path = '\\\\.\\pipe\\sshush-e2e-%s'\n", randomSuffix(t))
	if opts.vault {
		win.WriteString("type = \"vault\"\n")
	} else {
		win.WriteString("type = \"keys\"\n")
	}
	if opts.windowsKeyComment != "" {
		fmt.Fprintf(&win, "key_paths = ['%s']\n", r.windowsPath(r.writeWindowsKey("id_ed25519", opts.windowsKeyComment)))
	}
	if opts.vault {
		// The file need not exist for wslpath, but its directory does.
		fmt.Fprintf(&win, "\n[vault]\nvault_path = '%s\\vault.json'\n", r.windowsPath(winDir))
	}
	winConfigDir := filepath.Join(winDir, "sshush")
	if err := os.MkdirAll(winConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(winConfigDir, "config.toml"), []byte(win.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	var linux strings.Builder
	linux.WriteString("[agent]\ntype = \"windows\"\n")
	fmt.Fprintf(&linux, "socket_path = %q\n", r.socketPath)
	if opts.findOnPath {
		t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+winBinDir)
	} else {
		fmt.Fprintf(&linux, "windows_sshush = %q\n", filepath.Join(winBinDir, "sshush.exe"))
	}
	r.configPath = filepath.Join(r.dir, "config.toml")
	if err := os.WriteFile(r.configPath, []byte(linux.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	// Stopping the relay does not stop the agent behind it, so each side is
	// stopped by its own sshush.
	t.Cleanup(func() {
		r.linux(nil, "stop")
		r.windows(nil, "stop")
	})

	if opts.vault {
		passphrase := strings.TrimSuffix(e2ePassphrase, "\n")
		if _, stderr, code := r.windows(strings.NewReader(passphrase+"\n"+passphrase+"\n"), "vault", "init", "--no-recovery"); code != 0 {
			t.Fatalf("sshush.exe vault init: exit %d\nstderr: %s", code, stderr)
		}
	}
	return r
}

// linux runs the sshush in the distro.
func (r *wslRig) linux(stdin io.Reader, args ...string) (stdout, stderr string, exitCode int) {
	r.t.Helper()
	return runSSHush(r.t, r.binDir, r.configPath, r.dir, stdin, args...)
}

// windows runs sshush.exe directly, as PowerShell would, against the Windows
// side's own config. It runs from the binary's directory for the reason the
// bridge does: a Windows process handed a Linux working directory gets a
// \\wsl.localhost path, which some of Windows refuses.
func (r *wslRig) windows(stdin io.Reader, args ...string) (stdout, stderr string, exitCode int) {
	r.t.Helper()
	cmd := exec.Command(filepath.Join(r.winBinDir, "sshush.exe"), args...)
	cmd.Dir = r.winBinDir
	cmd.Stdin = stdin
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		exitCode = -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// windowsPath returns path, a /mnt/... path here, as Windows names it.
func (r *wslRig) windowsPath(path string) string {
	r.t.Helper()
	out, err := exec.Command("wslpath", "-w", path).Output()
	if err != nil {
		r.t.Fatalf("wslpath -w %s: %v", path, err)
	}
	return strings.TrimSpace(string(out))
}

// writeWindowsKey writes a key on the Windows side and returns its path here.
func (r *wslRig) writeWindowsKey(filename, comment string) string {
	r.t.Helper()
	return writeE2ETestKey(r.t, r.winDir, filename, comment)
}

// start runs "sshush start" in the distro, which brings up the relay and, when
// nothing answers through it, the agent on Windows.
func (r *wslRig) start(stdin io.Reader) (stderr string) {
	r.t.Helper()
	_, stderr, code := r.linux(stdin, "start")
	if code != 0 {
		r.t.Fatalf("sshush start in the distro: exit %d\nstderr: %s", code, stderr)
	}
	return stderr
}

// agentKeys lists what the agent holds as ssh in the distro would see it: the
// agent protocol on the distro's socket.
func (r *wslRig) agentKeys() (keys []*sshagent.Key, client sshagent.ExtendedAgent) {
	r.t.Helper()
	conn, err := net.Dial("unix", r.socketPath)
	if err != nil {
		r.t.Fatalf("dial the distro's socket: %v", err)
	}
	r.t.Cleanup(func() { _ = conn.Close() })
	// Generous for a Windows process per connection on a cold runner, and still
	// far short of the suite's timeout: a relay that drops one direction fails
	// here rather than hanging.
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	client = sshagent.NewClient(conn)
	keys, err = client.List()
	if err != nil {
		r.t.Fatalf("list through the distro's socket: %v", err)
	}
	return keys, client
}

func comments(keys []*sshagent.Key) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = k.Comment
	}
	return out
}

func hasComment(keys []*sshagent.Key, comment string) bool {
	for _, k := range keys {
		if k.Comment == comment {
			return true
		}
	}
	return false
}

func TestE2E_WSL_StartFromColdBringsUpBothSides(t *testing.T) {
	r := newWSLRig(t, wslRigOptions{windowsKeyComment: "wsl-cold-start"})

	stderr := r.start(nil)
	if !strings.Contains(stderr, "relaying to the sshush agent on Windows") {
		t.Errorf("start should say it relays to Windows; stderr: %s", stderr)
	}

	stdout, stderr, code := r.linux(nil, "list")
	if code != 0 {
		t.Fatalf("list in the distro: exit %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "wsl-cold-start") {
		t.Errorf("the distro should list the key the Windows agent holds; got: %s", stdout)
	}
	// The Windows side was started by the distro, not by this test.
	stdout, stderr, code = r.windows(nil, "list")
	if code != 0 {
		t.Fatalf("sshush.exe list: exit %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "wsl-cold-start") {
		t.Errorf("sshush.exe should list the same key; got: %s", stdout)
	}
}

func TestE2E_WSL_SignsThroughTheRelay(t *testing.T) {
	r := newWSLRig(t, wslRigOptions{windowsKeyComment: "wsl-sign"})
	r.start(nil)

	keys, client := r.agentKeys()
	if len(keys) != 1 || keys[0].Comment != "wsl-sign" {
		t.Fatalf("want the one key the Windows agent holds, got %v", comments(keys))
	}
	data := []byte("signed on Windows for a client in WSL")
	sig, err := client.Sign(keys[0], data)
	if err != nil {
		t.Fatalf("sign through the relay: %v", err)
	}
	if err := keys[0].Verify(data, sig); err != nil {
		t.Fatalf("verify signature: %v", err)
	}
}

func TestE2E_WSL_KeysAddedOnEitherSideShowOnTheOther(t *testing.T) {
	// A keys agent's config has to name a key, so it starts with one.
	r := newWSLRig(t, wslRigOptions{windowsKeyComment: "wsl-first-key"})
	r.start(nil)

	winKey := r.windowsPath(r.writeWindowsKey("id_added_on_windows", "wsl-added-on-windows"))
	if _, stderr, code := r.windows(nil, "add", winKey); code != 0 {
		t.Fatalf("sshush.exe add: exit %d\nstderr: %s", code, stderr)
	}
	if keys, _ := r.agentKeys(); !hasComment(keys, "wsl-added-on-windows") {
		t.Errorf("a key added on Windows should show in the distro; got %v", comments(keys))
	}

	// The key file is on the Linux filesystem, which Windows never reads: the
	// distro's sshush sends its contents.
	linuxKey := writeE2ETestKey(t, r.dir, "id_added_in_wsl", "wsl-added-in-distro")
	if _, stderr, code := r.linux(nil, "add", linuxKey); code != 0 {
		t.Fatalf("add in the distro: exit %d\nstderr: %s", code, stderr)
	}
	stdout, stderr, code := r.windows(nil, "list")
	if code != 0 {
		t.Fatalf("sshush.exe list: exit %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "wsl-added-in-distro") {
		t.Errorf("a key added in the distro should show on Windows; got: %s", stdout)
	}
}

func TestE2E_WSL_VaultLockAndUnlockAreShared(t *testing.T) {
	r := newWSLRig(t, wslRigOptions{vault: true})

	// This run starts both halves and the vault is locked, so start asks for
	// the passphrase and unlocks the Windows vault through the relay.
	r.start(strings.NewReader(e2ePassphrase))

	key := writeE2ETestKey(t, r.dir, "id_vault", "wsl-vault-key")
	if _, stderr, code := r.linux(nil, "add", key); code != 0 {
		t.Fatalf("add in the distro (vault should be unlocked by start): exit %d\nstderr: %s", code, stderr)
	}
	if stdout, _, _ := r.windows(nil, "list"); !strings.Contains(stdout, "wsl-vault-key") {
		t.Fatalf("Windows should hold the key added from the distro; got: %s", stdout)
	}

	if _, stderr, code := r.linux(nil, "lock"); code != 0 {
		t.Fatalf("lock in the distro: exit %d\nstderr: %s", code, stderr)
	}
	if stdout, _, _ := r.windows(nil, "list"); strings.Contains(stdout, "wsl-vault-key") {
		t.Errorf("locking in the distro should lock the vault on Windows; sshush.exe list: %s", stdout)
	}

	if _, stderr, code := r.windows(strings.NewReader(e2ePassphrase), "unlock"); code != 0 {
		t.Fatalf("sshush.exe unlock: exit %d\nstderr: %s", code, stderr)
	}
	if keys, _ := r.agentKeys(); !hasComment(keys, "wsl-vault-key") {
		t.Errorf("unlocking on Windows should unlock for the distro; got %v", comments(keys))
	}
}

func TestE2E_WSL_StopInTheDistroLeavesTheWindowsAgentRunning(t *testing.T) {
	r := newWSLRig(t, wslRigOptions{windowsKeyComment: "wsl-stop"})
	r.start(nil)

	if _, stderr, code := r.linux(nil, "stop"); code != 0 {
		t.Fatalf("stop in the distro: exit %d\nstderr: %s", code, stderr)
	}
	if conn, err := net.Dial("unix", r.socketPath); err == nil {
		_ = conn.Close()
		t.Error("the relay's socket should be gone after stop")
	}
	stdout, stderr, code := r.windows(nil, "list")
	if code != 0 {
		t.Fatalf("the Windows agent should still answer after the distro's stop: exit %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "wsl-stop") {
		t.Errorf("the Windows agent should still hold its key; got: %s", stdout)
	}
}

func TestE2E_WSL_NamesTheWindowsAgentWhenItIsDown(t *testing.T) {
	r := newWSLRig(t, wslRigOptions{windowsKeyComment: "wsl-down"})
	r.start(nil)

	if _, stderr, code := r.windows(nil, "stop"); code != 0 {
		t.Fatalf("sshush.exe stop: exit %d\nstderr: %s", code, stderr)
	}
	_, stderr, code := r.linux(nil, "list")
	if code == 0 {
		t.Fatal("list in the distro should fail with the Windows agent stopped")
	}
	if !strings.Contains(stderr, "the sshush agent on Windows is not answering") {
		t.Errorf("list should say it is the Windows agent that is down; stderr: %s", stderr)
	}
}

func TestE2E_WSL_FindsSSHushExeOnPath(t *testing.T) {
	r := newWSLRig(t, wslRigOptions{windowsKeyComment: "wsl-on-path", findOnPath: true})
	r.start(nil)

	if keys, _ := r.agentKeys(); !hasComment(keys, "wsl-on-path") {
		t.Errorf("with sshush.exe found on PATH the distro should see the Windows key; got %v", comments(keys))
	}
}
