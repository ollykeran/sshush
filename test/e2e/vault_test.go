package e2e

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	glssh "github.com/gliderlabs/ssh"
	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

const e2ePassphrase = "e2epass\n"

var (
	buildOnce sync.Once
	binDir    string
	buildErr  error
)

func buildBins(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "sshush-e2e-bin-")
		if err != nil {
			buildErr = err
			return
		}
		binDir = dir
		moduleRoot, err := findModuleRoot()
		if err != nil {
			buildErr = err
			return
		}
		for _, target := range []struct{ name, pkg string }{
			{"sshush", "./cmd/sshush"},
			{"sshushd", "./cmd/sshushd"},
		} {
			outPath := filepath.Join(binDir, target.name)
			cmd := exec.Command("go", "build", "-o", outPath, target.pkg)
			cmd.Dir = moduleRoot
			if out, err := cmd.CombinedOutput(); err != nil {
				buildErr = fmt.Errorf("build %s: %w\n%s", target.name, err, out)
				return
			}
		}
	})
	if buildErr != nil {
		t.Skipf("build binaries: %v", buildErr)
	}
	return binDir
}

func findModuleRoot() (string, error) {
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}")
	cmd.Dir = "."
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go list -m: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// writeE2EConfig writes a TOML config with [agent] and optional [vault] and key_paths.
// Returns the path to the config file. dir is used as the config directory and for socket/vault paths.
func writeE2EConfig(t *testing.T, dir, socketPath, vaultPath string, keyPaths []string) string {
	t.Helper()
	configPath := filepath.Join(dir, "config.toml")
	var b strings.Builder
	b.WriteString("[agent]\n")
	b.WriteString(fmt.Sprintf("socket_path = %q\n", socketPath))
	if vaultPath != "" {
		b.WriteString("vault = true\n")
	} else {
		b.WriteString("vault = false\n")
	}
	if len(keyPaths) > 0 {
		quoted := make([]string, len(keyPaths))
		for i, p := range keyPaths {
			quoted[i] = fmt.Sprintf("%q", p)
		}
		b.WriteString("key_paths = [" + strings.Join(quoted, ", ") + "]\n")
	}
	if vaultPath != "" {
		b.WriteString("\n[vault]\n")
		b.WriteString(fmt.Sprintf("vault_path = %q\n", vaultPath))
	}
	body := b.String()
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

// runSSHush runs the sshush binary with the given config, stdin, and args.
// Prepends --config configPath so the test config is always used (overrides default path).
// Env: XDG_RUNTIME_DIR set to runtimeDir so pid file is isolated; SSHUSH_CONFIG also set for daemon child.
// If stdin is nil, no stdin is set (child gets /dev/null). Otherwise stdin is used as the child's stdin.
// Returns stdout, stderr, and exit code.
func runSSHush(t *testing.T, binDir, configPath, runtimeDir string, stdin io.Reader, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	sshushPath := filepath.Join(binDir, "sshush")
	fullArgs := append([]string{"--config", configPath}, args...)
	cmd := exec.Command(sshushPath, fullArgs...)
	env := make([]string, 0, len(os.Environ())+2)
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "SSHUSH_CONFIG=") || strings.HasPrefix(e, "XDG_RUNTIME_DIR=") {
			continue
		}
		env = append(env, e)
	}
	cmd.Env = append(env, "SSHUSH_CONFIG="+configPath, "XDG_RUNTIME_DIR="+runtimeDir)
	cmd.Stdin = stdin
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return stdout, stderr, exitCode
}

// writeE2ETestKey generates an ed25519 key and writes private+public to dir. Returns private key path.
func writeE2ETestKey(t *testing.T, dir, filename, comment string) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		t.Fatal(err)
	}
	privPEM := pem.EncodeToMemory(block)
	privPath := filepath.Join(dir, filename)
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pubAuth := ssh.MarshalAuthorizedKey(signer.PublicKey())
	if err := os.WriteFile(privPath+".pub", pubAuth, 0o644); err != nil {
		t.Fatal(err)
	}
	return privPath
}

// startE2ESSHServer starts a gliderlabs SSH server that accepts only the given public key.
// Returns the listen address and a cleanup function that closes the server.
func startE2ESSHServer(t *testing.T, authorizedKey ssh.PublicKey) (addr string, cleanup func()) {
	t.Helper()
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	authorizedBlob := authorizedKey.Marshal()
	server := &glssh.Server{
		Handler: func(s glssh.Session) {
			fmt.Fprintln(s, "authenticated")
		},
		PublicKeyHandler: func(ctx glssh.Context, key glssh.PublicKey) bool {
			clientBlob := key.Marshal()
			return len(clientBlob) == len(authorizedBlob) &&
				subtle.ConstantTimeCompare(clientBlob, authorizedBlob) == 1
		},
	}
	server.AddHostKey(hostSigner)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve(ln)
	for i := 0; i < 50; i++ {
		conn, err := net.DialTimeout("tcp", ln.Addr().String(), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return ln.Addr().String(), func() { server.Close() }
}

func TestE2E_VaultLifecycle(t *testing.T) {
	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	vaultPath := filepath.Join(dir, "vault.json")

	binDir := buildBins(t)
	configPath := writeE2EConfig(t, dir, socketPath, vaultPath, nil)
	runtimeDir := dir

	// vault init: passphrase + confirm (two lines); use file so child reads both lines reliably
	initStdinFile := filepath.Join(dir, "init_stdin.txt")
	if err := os.WriteFile(initStdinFile, []byte("e2epass\ne2epass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	initStdin, err := os.Open(initStdinFile)
	if err != nil {
		t.Fatal(err)
	}
	defer initStdin.Close()
	_, stderr, code := runSSHush(t, binDir, configPath, runtimeDir, initStdin, "vault", "init", "--no-recovery")
	if code != 0 {
		t.Fatalf("vault init: exit %d\nstderr: %s", code, stderr)
	}

	// start: one passphrase for unlock
	_, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, strings.NewReader(e2ePassphrase), "start")
	if code != 0 {
		t.Fatalf("start: exit %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "started") && !strings.Contains(stderr, socketPath) {
		t.Errorf("start stderr should contain 'started' or socket path; got: %s", stderr)
	}

	// add key
	keyPath := writeE2ETestKey(t, dir, "id_ed25519", "e2e-key")
	_, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "add", keyPath)
	if code != 0 {
		t.Fatalf("add: exit %d\nstderr: %s", code, stderr)
	}

	// vault list: should show the key
	stdout, stderr, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "vault", "list")
	if code != 0 {
		t.Fatalf("vault list: exit %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "e2e-key") {
		t.Errorf("vault list should contain 'e2e-key'; got stdout: %s", stdout)
	}

	// Signing: dial socket, list keys, sign data, verify
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial agent: %v", err)
	}
	agentClient := sshagent.NewClient(conn)
	keys, err := agentClient.List()
	if err != nil {
		conn.Close()
		t.Fatalf("agent list: %v", err)
	}
	if len(keys) < 1 {
		conn.Close()
		t.Fatalf("expected at least one key, got %d", len(keys))
	}
	data := []byte("e2e-sign-test")
	sig, err := agentClient.Sign(keys[0], data)
	if err != nil {
		conn.Close()
		t.Fatalf("agent sign: %v", err)
	}
	if err := keys[0].Verify(data, sig); err != nil {
		conn.Close()
		t.Fatalf("verify signature: %v", err)
	}
	savedKey := keys[0]
	conn.Close()

	// Lock
	_, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "lock")
	if code != 0 {
		t.Fatalf("lock: exit %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "locked") {
		t.Errorf("lock stderr should contain 'locked'; got: %s", stderr)
	}

	// While locked: List is empty (SSH agent protocol); Sign still fails without master key
	lockedConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial agent when locked: %v", err)
	}
	defer lockedConn.Close()
	lockedClient := sshagent.NewClient(lockedConn)
	lockedKeys, err := lockedClient.List()
	if err != nil {
		t.Fatalf("list when locked: %v", err)
	}
	if len(lockedKeys) != 0 {
		t.Errorf("list when locked: want 0 keys, got %d", len(lockedKeys))
	}
	_, signErr := lockedClient.Sign(savedKey, []byte("test"))
	if signErr == nil {
		t.Error("Sign should fail when vault is locked")
	}

	// Unlock
	_, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, strings.NewReader(e2ePassphrase), "unlock")
	if code != 0 {
		t.Fatalf("unlock: exit %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "unlocked") {
		t.Errorf("unlock stderr should contain 'unlocked'; got: %s", stderr)
	}

	// After unlock: list should show the key again
	stdout, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "vault", "list")
	if code != 0 {
		t.Fatalf("vault list after unlock: exit %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "e2e-key") {
		t.Errorf("vault list after unlock should contain 'e2e-key'; got: %s", stdout)
	}

	// SSH connect: start server authorizing vault key, connect with agent auth
	pubBytes, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("read pub key: %v", err)
	}
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey(pubBytes)
	if err != nil {
		t.Fatalf("parse authorized key: %v", err)
	}
	serverAddr, closeServer := startE2ESSHServer(t, pubKey)
	defer closeServer()

	agentConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial agent for ssh: %v", err)
	}
	defer agentConn.Close()
	agentSigners := sshagent.NewClient(agentConn)
	clientConfig := &ssh.ClientConfig{
		User: "e2e",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeysCallback(agentSigners.Signers),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	sshClient, err := ssh.Dial("tcp", serverAddr, clientConfig)
	if err != nil {
		t.Fatalf("SSH dial: %v", err)
	}
	defer sshClient.Close()
	session, err := sshClient.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer session.Close()
	var out bytes.Buffer
	session.Stdout = &out
	if err := session.Run(""); err != nil {
		t.Fatalf("session run: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("authenticated")) {
		t.Errorf("expected 'authenticated' in session output; got: %s", out.String())
	}

	// stop daemon
	_, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "stop")
	if code != 0 {
		t.Errorf("stop: exit %d\nstderr: %s", code, stderr)
	}
}

func TestE2E_VaultSubcommandManage(t *testing.T) {
	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	vaultPath := filepath.Join(dir, "vault.json")
	binDir := buildBins(t)
	configPath := writeE2EConfig(t, dir, socketPath, vaultPath, nil)
	runtimeDir := dir

	initStdinFile := filepath.Join(dir, "init_stdin.txt")
	if err := os.WriteFile(initStdinFile, []byte("e2epass\ne2epass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	initStdin, err := os.Open(initStdinFile)
	if err != nil {
		t.Fatal(err)
	}
	defer initStdin.Close()
	_, stderr, code := runSSHush(t, binDir, configPath, runtimeDir, initStdin, "vault", "init", "--no-recovery")
	if code != 0 {
		t.Fatalf("vault init: exit %d\nstderr: %s", code, stderr)
	}

	_, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, strings.NewReader(e2ePassphrase), "start")
	if code != 0 {
		t.Fatalf("start: exit %d\nstderr: %s", code, stderr)
	}

	keyNoLoad := writeE2ETestKey(t, dir, "id_noload", "e2e-noload")
	_, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "vault", "add", "--no-autoload", keyNoLoad)
	if code != 0 {
		t.Fatalf("vault add: exit %d\nstderr: %s", code, stderr)
	}

	_, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "stop")
	if code != 0 {
		t.Fatalf("stop: exit %d\nstderr: %s", code, stderr)
	}

	_, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, strings.NewReader(e2ePassphrase), "start")
	if code != 0 {
		t.Fatalf("start after stop: exit %d\nstderr: %s", code, stderr)
	}

	stdout, stderr, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "list")
	if code != 0 {
		t.Fatalf("list: exit %d\nstderr: %s", code, stderr)
	}
	if strings.Contains(stdout, "e2e-noload") {
		t.Errorf("after restart, agent list should not include non-autoload key; got: %s", stdout)
	}

	stdout, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "vault", "list")
	if code != 0 {
		t.Fatalf("vault list: exit %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "e2e-noload") {
		t.Errorf("vault list should still show identity; got: %s", stdout)
	}

	_, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "vault", "load", "e2e-noload")
	if code != 0 {
		t.Fatalf("vault load: exit %d\nstderr: %s", code, stderr)
	}

	stdout, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "list")
	if code != 0 {
		t.Fatalf("list after load: exit %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "e2e-noload") {
		t.Errorf("after vault load, agent list should include key; got: %s", stdout)
	}

	_, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "vault", "remove", "e2e-noload")
	if code != 0 {
		t.Fatalf("vault remove: exit %d\nstderr: %s", code, stderr)
	}

	stdout, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "vault", "list")
	if code != 0 {
		t.Fatalf("vault list after remove: exit %d\nstderr: %s", code, stderr)
	}
	if strings.Contains(stdout, "e2e-noload") {
		t.Errorf("vault list should be empty of removed key; got: %s", stdout)
	}

	// autoload toggle: add with default autoload on
	keyAuto := writeE2ETestKey(t, dir, "id_auto", "e2e-autoload-toggle")
	_, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "vault", "add", keyAuto)
	if code != 0 {
		t.Fatalf("vault add autoload on: exit %d\nstderr: %s", code, stderr)
	}

	_, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "vault", "autoload", "off", "e2e-autoload-toggle")
	if code != 0 {
		t.Fatalf("vault autoload off: exit %d\nstderr: %s", code, stderr)
	}

	_, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "stop")
	if code != 0 {
		t.Fatalf("stop: exit %d\nstderr: %s", code, stderr)
	}
	_, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, strings.NewReader(e2ePassphrase), "start")
	if code != 0 {
		t.Fatalf("start: exit %d\nstderr: %s", code, stderr)
	}

	stdout, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "list")
	if code != 0 {
		t.Fatalf("list: exit %d\nstderr: %s", code, stderr)
	}
	if strings.Contains(stdout, "e2e-autoload-toggle") {
		t.Errorf("after autoload off and restart, key should not list; got: %s", stdout)
	}

	_, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "vault", "autoload", "on", "e2e-autoload-toggle")
	if code != 0 {
		t.Fatalf("vault autoload on: exit %d\nstderr: %s", code, stderr)
	}

	_, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "stop")
	if code != 0 {
		t.Fatalf("stop: exit %d\nstderr: %s", code, stderr)
	}
	_, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, strings.NewReader(e2ePassphrase), "start")
	if code != 0 {
		t.Fatalf("start: exit %d\nstderr: %s", code, stderr)
	}

	stdout, stderr, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "list")
	if code != 0 {
		t.Fatalf("list: exit %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "e2e-autoload-toggle") {
		t.Errorf("after autoload on and restart, key should list; got: %s", stdout)
	}

	_, _, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "stop")
	if code != 0 {
		t.Errorf("final stop: exit %d", code)
	}
}

func TestE2E_VaultFallbackToKeyPaths(t *testing.T) {
	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	vaultPath := filepath.Join(dir, "nonexistent-vault.json") // does not exist
	keyPath := writeE2ETestKey(t, dir, "id_ed25519", "fallback-key")

	binDir := buildBins(t)
	configPath := writeE2EConfig(t, dir, socketPath, vaultPath, []string{keyPath})
	runtimeDir := dir

	// start without vault file: should fall back to key_paths and warn
	_, stderr, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "start")
	if code != 0 {
		t.Fatalf("start: exit %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "vault") || !strings.Contains(stderr, "key_paths") {
		t.Errorf("stderr should mention vault and key_paths; got: %s", stderr)
	}

	// list should show the key from key_paths
	stdout, _, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "list")
	if code != 0 {
		t.Fatalf("list: exit %d", code)
	}
	if !strings.Contains(stdout, "fallback-key") {
		t.Errorf("list should contain 'fallback-key'; got: %s", stdout)
	}

	_, _, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "stop")
	if code != 0 {
		t.Errorf("stop: exit %d", code)
	}
}

func TestE2E_VaultMissingNoKeys(t *testing.T) {
	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	vaultPath := filepath.Join(dir, "nonexistent-vault.json") // does not exist
	// no key_paths or invalid key_paths
	configPath := writeE2EConfig(t, dir, socketPath, vaultPath, nil)

	binDir := buildBins(t)
	runtimeDir := dir

	_, stderr, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "start")
	if code == 0 {
		t.Fatal("start should fail when vault missing and no key_paths")
	}
	if !strings.Contains(stderr, "vault") || !strings.Contains(stderr, "init") {
		t.Errorf("stderr should suggest vault init; got: %s", stderr)
	}
}
