package e2e

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
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

func TestE2E_ServerStartStop(t *testing.T) {
	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	vaultPath := filepath.Join(dir, "vault.json")
	serverPort := 22402

	binDir := buildBins(t)
	configPath := writeE2EConfigWithServer(t, dir, socketPath, vaultPath, nil, serverPort, "", "")
	runtimeDir := dir

	// Vault init
	if err := os.WriteFile(filepath.Join(dir, "init_stdin.txt"), []byte("e2epass\ne2epass\n"), 0o600); err != nil {
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

	// Vault init
	initStdin := strings.NewReader("e2epass\ne2epass\n")
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
	_, _, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "add", keyPath, "--auto")
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
		User: "e2e",
		Auth: []ssh.AuthMethod{ssh.PublicKeysCallback(signers.Signers)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	sshConn, err := ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), clientConfig)
	if err != nil {
		t.Fatalf("SSH dial: %v", err)
	}
	defer sshConn.Close()
	session, err := sshConn.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer session.Close()
	stdout, err := session.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := session.Shell(); err != nil {
		t.Fatalf("shell: %v", err)
	}
	var out bytes.Buffer
	out.ReadFrom(stdout)
	session.Close()
	if !bytes.Contains(out.Bytes(), []byte("sshush session (authorized by key)")) {
		t.Errorf("expected session message; got: %s", out.String())
	}

	runSSHush(t, binDir, configPath, runtimeDir, nil, "server", "stop")
	runSSHush(t, binDir, configPath, runtimeDir, nil, "stop")
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
	session, err := sshConn.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer session.Close()
	stdout, _ := session.StdoutPipe()
	_ = session.Shell()
	var out bytes.Buffer
	out.ReadFrom(stdout)
	session.Close()
	if !bytes.Contains(out.Bytes(), []byte("sshush session (authorized by key)")) {
		t.Errorf("expected session message; got: %s", out.String())
	}

	runSSHush(t, binDir, configPath, runtimeDir, nil, "server", "stop")
	runSSHush(t, binDir, configPath, runtimeDir, nil, "stop")
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

	runSSHush(t, binDir, configPath, runtimeDir, nil, "server", "stop")
	runSSHush(t, binDir, configPath, runtimeDir, nil, "stop")
}

func TestE2E_ServerAddKeyThenConnect(t *testing.T) {
	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	vaultPath := filepath.Join(dir, "vault.json")
	serverPort := 22406

	binDir := buildBins(t)
	configPath := writeE2EConfigWithServer(t, dir, socketPath, vaultPath, nil, serverPort, "", "")
	runtimeDir := dir

	initStdin := strings.NewReader("e2epass\ne2epass\n")
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
	_, _, code = runSSHush(t, binDir, configPath, runtimeDir, nil, "add", keyPath, "--auto")
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
		User: "e2e",
		Auth: []ssh.AuthMethod{ssh.PublicKeysCallback(signers.Signers)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	sshConn, err := ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), clientConfig)
	if err != nil {
		t.Fatalf("SSH dial: %v", err)
	}
	defer sshConn.Close()
	session, err := sshConn.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer session.Close()
	stdout, _ := session.StdoutPipe()
	_ = session.Shell()
	var out bytes.Buffer
	out.ReadFrom(stdout)
	session.Close()
	if !bytes.Contains(out.Bytes(), []byte("sshush session (authorized by key)")) {
		t.Errorf("expected session message after add key; got: %s", out.String())
	}

	runSSHush(t, binDir, configPath, runtimeDir, nil, "server", "stop")
	runSSHush(t, binDir, configPath, runtimeDir, nil, "stop")
}
