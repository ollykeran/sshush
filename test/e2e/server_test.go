package e2e

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// writeE2EConfigWithServer writes a TOML config like writeE2EConfig plus optional [server] fields.
func writeE2EConfigWithServer(t *testing.T, dir, socketPath, vaultPath string, keyPaths []string, serverPort int, authorizedKeysPath, hostKeyPath string) string {
	t.Helper()
	configPath := filepath.Join(dir, "config.toml")
	var b strings.Builder
	b.WriteString("[agent]\n")
	b.WriteString(fmt.Sprintf("socket_path = %q\n", socketPath))
	if vaultPath != "" {
		b.WriteString("type = \"vault\"\n")
	} else {
		b.WriteString("type = \"keys\"\n")
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
	if serverPort > 0 || authorizedKeysPath != "" || hostKeyPath != "" {
		b.WriteString("\n[server]\n")
		if serverPort > 0 {
			b.WriteString(fmt.Sprintf("listen_port = %d\n", serverPort))
		}
		if authorizedKeysPath != "" {
			b.WriteString(fmt.Sprintf("authorized_keys = %q\n", authorizedKeysPath))
		}
		if hostKeyPath != "" {
			b.WriteString(fmt.Sprintf("host_key = %q\n", hostKeyPath))
		}
	}
	if err := os.WriteFile(configPath, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

// openPtyShell opens a session on conn, requests a pty of the given size and
// starts the shell, returning the pipes needed to drive it.
func openPtyShell(t *testing.T, conn *ssh.Client, rows, cols int) (*ssh.Session, io.WriteCloser, io.Reader) {
	t.Helper()
	session, err := conn.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	stdin, err := session.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := session.RequestPty("xterm", rows, cols, ssh.TerminalModes{}); err != nil {
		t.Fatalf("request pty: %v", err)
	}
	if err := session.Shell(); err != nil {
		t.Fatalf("shell: %v", err)
	}
	return session, stdin, stdout
}

// runInPtyShell types command into a fresh pty shell on conn and waits for want
// to appear in its output.
func runInPtyShell(t *testing.T, conn *ssh.Client, command, want string) {
	t.Helper()
	_, stdin, stdout := openPtyShell(t, conn, 24, 80)
	if _, err := io.WriteString(stdin, command); err != nil {
		t.Fatalf("write to shell: %v", err)
	}
	waitForShellOutput(t, stdout, regexp.MustCompile(regexp.QuoteMeta(want)))
}

// waitForShellOutput reads the shell's output until re matches, failing on
// timeout with everything seen so far.
func waitForShellOutput(t *testing.T, stdout io.Reader, re *regexp.Regexp) string {
	t.Helper()
	chunks := make(chan []byte, 64)
	go func() {
		defer close(chunks)
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				chunks <- chunk
			}
			if err != nil {
				return
			}
		}
	}()

	var seen strings.Builder
	deadline := time.After(15 * time.Second)
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				t.Fatalf("shell output ended before %s matched; got:\n%s", re, seen.String())
			}
			seen.Write(chunk)
			if re.MatchString(seen.String()) {
				return seen.String()
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s; got:\n%s", re, seen.String())
		}
	}
}

func TestE2E_ServerStartStop(t *testing.T) {
	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	vaultPath := filepath.Join(dir, "vault.json")
	serverPort := 22402

	binDir := buildBins(t)
	configPath := writeE2EConfigWithServer(t, dir, socketPath, vaultPath, nil, serverPort, "", "")
	runtimeDir := dir
	stopDaemonsAtCleanup(t, binDir, configPath, runtimeDir)

	// Vault init
	if err := os.WriteFile(filepath.Join(dir, "init_stdin.txt"), []byte("e2epassthatalwaysmeetspasswordrequirementsA1!\ne2epassthatalwaysmeetspasswordrequirementsA1!\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	initStdin, err := os.Open(filepath.Join(dir, "init_stdin.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer initStdin.Close()
	_, _, code := runSSHush(t, binDir, configPath, runtimeDir, initStdin, "vault", "init", "--no-recovery")
	if code != 0 {
		t.Fatalf("vault init: exit %d", code)
	}

	// Start agent
	_, _, code = runSSHush(t, binDir, configPath, runtimeDir, strings.NewReader(e2ePassphrase), "start")
	if code != 0 {
		t.Fatalf("start: exit %d", code)
	}

	// Start server
	stdout, stderr, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "server")
	if code != 0 {
		t.Fatalf("server start: exit %d\nstderr: %s", code, stderr)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "started") && !strings.Contains(combined, "22402") {
		t.Errorf("server output should contain started or port; got stdout: %q stderr: %q", stdout, stderr)
	}

	// Port should be listening
	addr := fmt.Sprintf("127.0.0.1:%d", serverPort)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("server port not listening: %v", err)
	}
	conn.Close()

	// Stop server
	_, _, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "server", "stop")
	if code != 0 {
		t.Fatalf("server stop: exit %d", code)
	}

	// Port should be closed
	for i := 0; i < 20; i++ {
		if c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond); err != nil {
			break
		} else {
			c.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	if conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
		conn.Close()
		t.Error("server port should be closed after server stop")
	}

	// Stop agent
	runSSHush(t, binDir, configPath, runtimeDir, nil, "stop")
}

func TestE2E_ServerConnectAgentAuth(t *testing.T) {
	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	vaultPath := filepath.Join(dir, "vault.json")
	serverPort := 22403

	binDir := buildBins(t)
	configPath := writeE2EConfigWithServer(t, dir, socketPath, vaultPath, nil, serverPort, "", "")
	runtimeDir := dir
	stopDaemonsAtCleanup(t, binDir, configPath, runtimeDir)

	// Vault init
	initStdin := strings.NewReader("e2epassthatalwaysmeetspasswordrequirementsA1!\ne2epassthatalwaysmeetspasswordrequirementsA1!\n")
	_, _, code := runSSHush(t, binDir, configPath, runtimeDir, initStdin, "vault", "init", "--no-recovery")
	if code != 0 {
		t.Fatalf("vault init: exit %d", code)
	}
	// Start agent
	_, _, code = runSSHush(t, binDir, configPath, runtimeDir, strings.NewReader(e2ePassphrase), "start")
	if code != 0 {
		t.Fatalf("start: exit %d", code)
	}
	// Add key
	keyPath := writeE2ETestKey(t, dir, "id_ed25519", "e2e-server-key")
	_, _, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "add", keyPath)
	if code != 0 {
		t.Fatalf("add: exit %d", code)
	}
	// Start server
	_, _, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "server")
	if code != 0 {
		t.Fatalf("server: exit %d", code)
	}

	// Connect with agent auth
	agentConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial agent: %v", err)
	}
	defer agentConn.Close()
	signers := sshagent.NewClient(agentConn)
	clientConfig := &ssh.ClientConfig{
		User:            "e2e",
		Auth:            []ssh.AuthMethod{ssh.PublicKeysCallback(signers.Signers)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	sshConn, err := ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), clientConfig)
	if err != nil {
		t.Fatalf("SSH dial: %v", err)
	}
	defer sshConn.Close()
	runInPtyShell(t, sshConn, "echo sshush-e2e-$((6*7))\n", "sshush-e2e-42")
}

func TestE2E_ServerFileAuth(t *testing.T) {
	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	keyPath := writeE2ETestKey(t, dir, "id_ed25519", "fileauth-key")
	serverPort := 22404
	authorizedKeysPath := filepath.Join(dir, "authorized_keys")

	pubBytes, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authorizedKeysPath, pubBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	binDir := buildBins(t)
	configPath := writeE2EConfigWithServer(t, dir, socketPath, "", []string{keyPath}, serverPort, authorizedKeysPath, "")
	runtimeDir := dir
	stopDaemonsAtCleanup(t, binDir, configPath, runtimeDir)

	// Start agent (so sshush server has a socket to connect to for config; actually for file auth we don't need agent)
	_, _, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "start")
	if code != 0 {
		t.Fatalf("start: exit %d", code)
	}
	// Start server with file auth
	_, _, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "server")
	if code != 0 {
		t.Fatalf("server: exit %d", code)
	}

	signer, err := readSignerFromFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	clientConfig := &ssh.ClientConfig{
		User:            "e2e",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	sshConn, err := ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), clientConfig)
	if err != nil {
		t.Fatalf("SSH dial: %v", err)
	}
	defer sshConn.Close()
	runInPtyShell(t, sshConn, "echo sshush-e2e-$((6*7))\n", "sshush-e2e-42")
}

// stopDaemonsAtCleanup stops the test's server and agent when the test ends,
// however it ends. A test that stopped them itself on its last lines would leave
// both running after any failure before then, still holding the fixed port the
// next run of that test needs.
func stopDaemonsAtCleanup(t *testing.T, binDir, configPath, runtimeDir string) {
	t.Helper()
	t.Cleanup(func() {
		runSSHush(t, binDir, configPath, runtimeDir, nil, "server", "stop")
		runSSHush(t, binDir, configPath, runtimeDir, nil, "stop")
	})
}

func readSignerFromFile(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(data)
}

func TestE2E_ServerHostKeyFile(t *testing.T) {
	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	keyPath := writeE2ETestKey(t, dir, "id_ed25519", "hostkey-key")
	serverPort := 22405
	hostKeyPath := filepath.Join(dir, "host_ed25519")

	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(hostPriv, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostKeyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}

	binDir := buildBins(t)
	configPath := writeE2EConfigWithServer(t, dir, socketPath, "", []string{keyPath}, serverPort, "", hostKeyPath)
	runtimeDir := dir
	stopDaemonsAtCleanup(t, binDir, configPath, runtimeDir)

	_, _, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "start")
	if code != 0 {
		t.Fatalf("start: exit %d", code)
	}
	_, _, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "server")
	if code != 0 {
		t.Fatalf("server: exit %d", code)
	}

	signer, err := readSignerFromFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	clientConfig := &ssh.ClientConfig{
		User:            "e2e",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	sshConn, err := ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), clientConfig)
	if err != nil {
		t.Fatalf("SSH dial: %v", err)
	}
	sshConn.Close()
}

func TestE2E_ServerAddKeyThenConnect(t *testing.T) {
	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	vaultPath := filepath.Join(dir, "vault.json")
	serverPort := 22406

	binDir := buildBins(t)
	configPath := writeE2EConfigWithServer(t, dir, socketPath, vaultPath, nil, serverPort, "", "")
	runtimeDir := dir
	stopDaemonsAtCleanup(t, binDir, configPath, runtimeDir)

	initStdin := strings.NewReader("e2epassthatalwaysmeetspasswordrequirementsA1!\ne2epassthatalwaysmeetspasswordrequirementsA1!\n")
	_, _, code := runSSHush(t, binDir, configPath, runtimeDir, initStdin, "vault", "init", "--no-recovery")
	if code != 0 {
		t.Fatalf("vault init: exit %d", code)
	}
	_, _, code = runSSHush(t, binDir, configPath, runtimeDir, strings.NewReader(e2ePassphrase), "start")
	if code != 0 {
		t.Fatalf("start: exit %d", code)
	}
	_, _, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "server")
	if code != 0 {
		t.Fatalf("server: exit %d", code)
	}

	// Add key after server is already running (agent has key, server uses agent auth)
	keyPath := writeE2ETestKey(t, dir, "id_ed25519", "added-later")
	_, _, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "add", keyPath)
	if code != 0 {
		t.Fatalf("add: exit %d", code)
	}

	agentConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial agent: %v", err)
	}
	defer agentConn.Close()
	signers := sshagent.NewClient(agentConn)
	clientConfig := &ssh.ClientConfig{
		User:            "e2e",
		Auth:            []ssh.AuthMethod{ssh.PublicKeysCallback(signers.Signers)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	sshConn, err := ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), clientConfig)
	if err != nil {
		t.Fatalf("SSH dial: %v", err)
	}
	defer sshConn.Close()
	runInPtyShell(t, sshConn, "echo sshush-e2e-$((6*7))\n", "sshush-e2e-42")
}

// TestE2E_ServerPtyShellResizes drives a real client through a window change and
// checks the shell sees the new size. It lives here rather than in the server
// package because the server runs out of process, where the race detector does
// not see gliderlabs' own write to the session's Pty on a window-change request.
func TestE2E_ServerPtyShellResizes(t *testing.T) {
	// The daemon inherits this test process's environment, so this fixes which
	// shell it runs.
	t.Setenv("SHELL", "/bin/sh")

	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	keyPath := writeE2ETestKey(t, dir, "id_ed25519", "resize-key")
	serverPort := 22407
	authorizedKeysPath := filepath.Join(dir, "authorized_keys")

	pubBytes, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authorizedKeysPath, pubBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	binDir := buildBins(t)
	configPath := writeE2EConfigWithServer(t, dir, socketPath, "", []string{keyPath}, serverPort, authorizedKeysPath, "")
	runtimeDir := dir

	if _, _, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "start"); code != 0 {
		t.Fatalf("start: exit %d", code)
	}
	if _, _, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "server"); code != 0 {
		t.Fatalf("server: exit %d", code)
	}
	t.Cleanup(func() {
		runSSHush(t, binDir, configPath, runtimeDir, nil, "server", "stop")
		runSSHush(t, binDir, configPath, runtimeDir, nil, "stop")
	})

	signer, err := readSignerFromFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	sshConn, err := ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), &ssh.ClientConfig{
		User:            "e2e",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("SSH dial: %v", err)
	}
	defer sshConn.Close()

	// The command is typed before the shell prints its first prompt, so the pty
	// echoes it straight away and the answer can land after a "$ " on the same
	// line. Prefix the answer with a marker the echoed command cannot match
	// rather than anchoring it to the start of a line.
	const sttySize = "echo size=$(stty size)\n"
	session, stdin, stdout := openPtyShell(t, sshConn, 30, 120)
	if _, err := io.WriteString(stdin, sttySize); err != nil {
		t.Fatalf("write to shell: %v", err)
	}
	waitForShellOutput(t, stdout, regexp.MustCompile(`(?m)size=30 120\r?$`))

	if err := session.WindowChange(40, 100); err != nil {
		t.Fatalf("window change: %v", err)
	}
	// The resize is a separate channel request, so ask again until it lands.
	go func() {
		for {
			if _, err := io.WriteString(stdin, sttySize); err != nil {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
	}()
	waitForShellOutput(t, stdout, regexp.MustCompile(`(?m)size=40 100\r?$`))
}

// TestE2E_ServerRunsARemoteCommand checks `ssh host <command>`: the command's
// output comes back, and so does its own exit status rather than a generic
// failure.
func TestE2E_ServerRunsARemoteCommand(t *testing.T) {
	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	keyPath := writeE2ETestKey(t, dir, "id_ed25519", "nopty-key")
	serverPort := 22408
	authorizedKeysPath := filepath.Join(dir, "authorized_keys")

	pubBytes, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authorizedKeysPath, pubBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	binDir := buildBins(t)
	configPath := writeE2EConfigWithServer(t, dir, socketPath, "", []string{keyPath}, serverPort, authorizedKeysPath, "")
	runtimeDir := dir

	if _, _, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "start"); code != 0 {
		t.Fatalf("start: exit %d", code)
	}
	if _, _, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "server"); code != 0 {
		t.Fatalf("server: exit %d", code)
	}
	t.Cleanup(func() {
		runSSHush(t, binDir, configPath, runtimeDir, nil, "server", "stop")
		runSSHush(t, binDir, configPath, runtimeDir, nil, "stop")
	})

	signer, err := readSignerFromFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	sshConn, err := ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), &ssh.ClientConfig{
		User:            "e2e",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("SSH dial: %v", err)
	}
	defer sshConn.Close()

	// Commands any login shell runs the same way: the daemon runs whatever
	// $SHELL it inherited from the test process.
	run := func(command string) (string, error) {
		t.Helper()
		session, err := sshConn.NewSession()
		if err != nil {
			t.Fatalf("new session: %v", err)
		}
		defer session.Close()
		var stdout strings.Builder
		session.Stdout = &stdout
		done := make(chan error, 1)
		go func() { done <- session.Run(command) }()
		select {
		case err := <-done:
			return stdout.String(), err
		case <-time.After(15 * time.Second):
			t.Fatalf("remote command %q did not finish", command)
			return "", nil
		}
	}

	stdout, err := run("echo hi")
	if err != nil {
		t.Fatalf("echo hi: %v", err)
	}
	if stdout != "hi\n" {
		t.Errorf("stdout = %q, want %q", stdout, "hi\n")
	}

	_, err = run("exit 3")
	var exitErr *ssh.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("exit 3: error = %v, want an *ssh.ExitError", err)
	}
	if exitErr.ExitStatus() != 3 {
		t.Errorf("exit status = %d, want 3", exitErr.ExitStatus())
	}
}

// TestE2E_ServerRefusesAShellThatCannotBeFound checks a bad [server].shell is
// reported by `sshush server` itself, rather than the daemon starting and then
// failing every connection.
func TestE2E_ServerRefusesAShellThatCannotBeFound(t *testing.T) {
	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	keyPath := writeE2ETestKey(t, dir, "id_ed25519", "bad-shell")
	serverPort := 22411
	authorizedKeysPath := filepath.Join(dir, "authorized_keys")

	pubBytes, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authorizedKeysPath, pubBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	binDir := buildBins(t)
	configPath := writeE2EConfigWithServer(t, dir, socketPath, "", []string{keyPath}, serverPort, authorizedKeysPath, "")
	appendToFile(t, configPath, "shell = \"/nonexistent/sshush-no-such-shell\"\n")
	runtimeDir := dir

	stdout, stderr, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "server")
	t.Cleanup(func() { runSSHush(t, binDir, configPath, runtimeDir, nil, "server", "stop") })
	if code == 0 {
		t.Fatalf("server started with a shell that does not exist; stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout+stderr, "sshush-no-such-shell") {
		t.Errorf("output does not name the missing shell; stdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), time.Second); err == nil {
		conn.Close()
		t.Error("something is listening on the server port after a refused start")
	}
}

// TestE2E_ServerSignsInWithTheVaultPassphrase covers [server].password_auth end to
// end, with no agent running at all: checking the passphrase needs the vault file
// and nothing else.
func TestE2E_ServerSignsInWithTheVaultPassphrase(t *testing.T) {
	const passphrase = "e2epassthatalwaysmeetspasswordrequirementsA1!"
	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	vaultPath := filepath.Join(dir, "vault.json")
	serverPort := 22412

	binDir := buildBins(t)
	configPath := writeE2EConfigWithServer(t, dir, socketPath, vaultPath, nil, serverPort, "", "")
	appendToFile(t, configPath, "password_auth = true\n")
	runtimeDir := dir

	initStdinFile := filepath.Join(dir, "init_stdin.txt")
	if err := os.WriteFile(initStdinFile, []byte(passphrase+"\n"+passphrase+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	initStdin, err := os.Open(initStdinFile)
	if err != nil {
		t.Fatal(err)
	}
	defer initStdin.Close()
	if _, stderr, code := runSSHush(t, binDir, configPath, runtimeDir, initStdin, "vault", "init", "--no-recovery"); code != 0 {
		t.Fatalf("vault init: exit %d\nstderr: %s", code, stderr)
	}

	if _, stderr, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "server"); code != 0 {
		t.Fatalf("server: exit %d\nstderr: %s", code, stderr)
	}
	t.Cleanup(func() { runSSHush(t, binDir, configPath, runtimeDir, nil, "server", "stop") })

	dial := func(password string) (*ssh.Client, error) {
		return ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), &ssh.ClientConfig{
			User:            "e2e",
			Auth:            []ssh.AuthMethod{ssh.Password(password)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         10 * time.Second,
		})
	}

	if conn, err := dial("not the passphrase"); err == nil {
		conn.Close()
		t.Error("a wrong password signed in")
	}

	conn, err := dial(passphrase)
	if err != nil {
		t.Fatalf("dial with the vault passphrase: %v", err)
	}
	defer conn.Close()
	session, err := conn.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer session.Close()
	output, err := session.Output("echo hi")
	if err != nil {
		t.Fatalf("echo hi: %v", err)
	}
	if string(output) != "hi\n" {
		t.Errorf("output = %q, want %q", output, "hi\n")
	}
}

// TestE2E_ServerLogsConnectionsAndSessions runs the daemon for real and reads back
// its log file: startup, a refused key, a sign-in and the session it ran, and the
// signal that stopped it — then checks `sshush server logs` prints the end of it.
func TestE2E_ServerLogsConnectionsAndSessions(t *testing.T) {
	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	keyPath := writeE2ETestKey(t, dir, "id_ed25519", "log-key")
	strangerPath := writeE2ETestKey(t, dir, "id_stranger", "stranger")
	serverPort := 22413
	authorizedKeysPath := filepath.Join(dir, "authorized_keys")
	logPath := filepath.Join(dir, "logs", "server.log")

	pubBytes, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authorizedKeysPath, pubBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	binDir := buildBins(t)
	configPath := writeE2EConfigWithServer(t, dir, socketPath, "", []string{keyPath}, serverPort, authorizedKeysPath, "")
	appendToFile(t, configPath, fmt.Sprintf("log_file = %q\n", logPath))
	runtimeDir := dir

	if _, stderr, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "server"); code != 0 {
		t.Fatalf("server: exit %d\nstderr: %s", code, stderr)
	}
	t.Cleanup(func() { runSSHush(t, binDir, configPath, runtimeDir, nil, "server", "stop") })

	dial := func(keyFile string) (*ssh.Client, error) {
		signer, err := readSignerFromFile(keyFile)
		if err != nil {
			t.Fatal(err)
		}
		return ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), &ssh.ClientConfig{
			User:            "e2e",
			Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         5 * time.Second,
		})
	}
	if conn, err := dial(strangerPath); err == nil {
		conn.Close()
		t.Fatal("a key not in authorized_keys signed in")
	}
	conn, err := dial(keyPath)
	if err != nil {
		t.Fatalf("SSH dial: %v", err)
	}
	session, err := conn.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if err := session.Run("true"); err != nil {
		t.Fatalf("true: %v", err)
	}
	session.Close()
	conn.Close()
	waitForLogLine(t, logPath, `msg="connection closed" remote=127\.0\.0\.1:\d+ user=e2e authenticated=true`)

	if _, stderr, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "server", "stop"); code != 0 {
		t.Fatalf("server stop: exit %d\nstderr: %s", code, stderr)
	}
	log := waitForLogLine(t, logPath, `level=INFO msg="server stopping" signal=terminated`)
	for _, pattern := range []string{
		`msg="server starting" version="sshushd [^"]+" pid=\d+ authorized_keys="?` + regexp.QuoteMeta(authorizedKeysPath),
		`msg="server listening" addr=\S+:22413 host_key_path=\S+ host_key=SHA256:\S+ auth_methods=publickey shell=\S+`,
		`msg="connection opened" remote=127\.0\.0\.1:\d+ local=127\.0\.0\.1:22413`,
		`msg="auth failed" method=publickey user=e2e remote=127\.0\.0\.1:\d+ key_type=ssh-ed25519 fingerprint=SHA256:\S+ methods_offered=publickey`,
		`msg="auth accepted" method=publickey user=e2e remote=127\.0\.0\.1:\d+ key_type=ssh-ed25519 fingerprint=SHA256:\S+`,
		`msg="session started" kind=command user=e2e remote=127\.0\.0\.1:\d+`,
		`msg="session closed" user=e2e remote=127\.0\.0\.1:\d+ exit_status=0`,
	} {
		if !regexp.MustCompile(pattern).MatchString(log) {
			t.Errorf("server log does not match %s:\n%s", pattern, log)
		}
	}

	stdout, stderr, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "server", "logs", "-n", "1")
	if code != 0 {
		t.Fatalf("server logs: exit %d\nstderr: %s", code, stderr)
	}
	if strings.Count(stdout, "\n") != 1 || !strings.Contains(stdout, `msg="server stopping" signal=terminated`) {
		t.Errorf("server logs -n 1 = %q, want just the last line", stdout)
	}
}

// waitForLogLine waits for the log file at path to match pattern and returns its
// content, failing with whatever it holds if that never happens.
func waitForLogLine(t *testing.T, path, pattern string) string {
	t.Helper()
	re := regexp.MustCompile(pattern)
	deadline := time.Now().Add(15 * time.Second)
	for {
		data, _ := os.ReadFile(path)
		if re.Match(data) {
			return string(data)
		}
		if time.Now().After(deadline) {
			t.Fatalf("server log never matched %s; got:\n%s", pattern, data)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// appendToFile appends text to the file at path. The [server] table is the last
// one writeE2EConfigWithServer writes, so a key appended here lands in it.
func appendToFile(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

// TestE2E_ServerKeepsItsHostKeyAcrossRestarts is the check behind persistent host
// keys: a client that pinned the host key on one run must not be told the host key
// changed on the next, which is what an ephemeral key caused.
func TestE2E_ServerKeepsItsHostKeyAcrossRestarts(t *testing.T) {
	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	keyPath := writeE2ETestKey(t, dir, "id_ed25519", "hostkey-persist")
	serverPort := 22409
	authorizedKeysPath := filepath.Join(dir, "authorized_keys")

	pubBytes, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authorizedKeysPath, pubBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	binDir := buildBins(t)
	// No host_key in the config: the point is that an unconfigured server still
	// keeps one identity.
	configPath := writeE2EConfigWithServer(t, dir, socketPath, "", []string{keyPath}, serverPort, authorizedKeysPath, "")
	runtimeDir := dir
	// The default host key lives in the config dir, so keep it inside the test's
	// own directory rather than the developer's real one.
	t.Setenv("XDG_CONFIG_HOME", dir)

	if _, _, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "start"); code != 0 {
		t.Fatalf("start: exit %d", code)
	}
	t.Cleanup(func() { runSSHush(t, binDir, configPath, runtimeDir, nil, "stop") })

	signer, err := readSignerFromFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	// hostKeyOffered starts the server, connects, and reports the host key the
	// client was given, then stops the server again.
	hostKeyOffered := func() string {
		t.Helper()
		if _, _, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "server"); code != 0 {
			t.Fatalf("server: exit %d", code)
		}
		defer runSSHush(t, binDir, configPath, runtimeDir, nil, "server", "stop")

		var offered string
		sshConn, err := ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), &ssh.ClientConfig{
			User: "e2e",
			Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
			HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
				offered = ssh.FingerprintSHA256(key)
				return nil
			},
			Timeout: 5 * time.Second,
		})
		if err != nil {
			t.Fatalf("SSH dial: %v", err)
		}
		sshConn.Close()
		return offered
	}

	first := hostKeyOffered()
	// The port takes a moment to free up between the two runs.
	for i := 0; i < 40; i++ {
		if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), 100*time.Millisecond); err != nil {
			break
		} else {
			c.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	second := hostKeyOffered()

	if first != second {
		t.Errorf("host key changed across restarts: %s then %s", first, second)
	}
	if _, err := os.Stat(filepath.Join(dir, "sshush", "server_host_ed25519")); err != nil {
		t.Errorf("expected a host key in the config dir: %v", err)
	}
}

// TestE2E_ServerStartsBeforeTheAgent covers the server being brought up with no
// agent to authenticate against: connections are refused until the agent appears,
// and it is picked up without restarting the server — including after the agent is
// stopped and started again.
func TestE2E_ServerStartsBeforeTheAgent(t *testing.T) {
	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	keyPath := writeE2ETestKey(t, dir, "id_ed25519", "later-agent")
	serverPort := 22410

	binDir := buildBins(t)
	// No authorized_keys: auth goes through the agent, which is not running yet.
	configPath := writeE2EConfigWithServer(t, dir, socketPath, "", []string{keyPath}, serverPort, "", "")
	runtimeDir := dir
	t.Setenv("XDG_CONFIG_HOME", dir)

	stdout, stderr, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "server")
	if code != 0 {
		t.Fatalf("server should start without an agent: exit %d\nstderr: %s", code, stderr)
	}
	if combined := stdout + stderr; !strings.Contains(combined, "No agent is running") {
		t.Errorf("starting without an agent should say so; got stdout: %q stderr: %q", stdout, stderr)
	}
	t.Cleanup(func() {
		runSSHush(t, binDir, configPath, runtimeDir, nil, "server", "stop")
		runSSHush(t, binDir, configPath, runtimeDir, nil, "stop")
	})

	signer, err := readSignerFromFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	// connects reports whether the key is accepted right now.
	connects := func() bool {
		t.Helper()
		conn, err := ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), &ssh.ClientConfig{
			User:            "e2e",
			Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         5 * time.Second,
		})
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}

	if connects() {
		t.Error("no key should be authorized while the agent is down")
	}

	if _, _, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "start"); code != 0 {
		t.Fatalf("start: exit %d", code)
	}
	if !connects() {
		t.Error("the key should be accepted once the agent is up, without restarting the server")
	}

	// A replaced agent is the case a connection held for the server's lifetime
	// would never recover from.
	if _, _, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "stop"); code != 0 {
		t.Fatalf("stop: exit %d", code)
	}
	if connects() {
		t.Error("no key should be authorized after the agent stops")
	}
	if _, _, code := runSSHush(t, binDir, configPath, runtimeDir, nil, "start"); code != 0 {
		t.Fatalf("restart agent: exit %d", code)
	}
	if !connects() {
		t.Error("the key should be accepted again after the agent is restarted")
	}
}
