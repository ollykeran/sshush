package e2e

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// These tests run the built "sshush relay" as a process against a running
// daemon, which is exactly what the sshush in WSL does with sshush.exe. On
// Windows the agent is on a named pipe, so this is the Windows half of the WSL
// relay with the real binaries and no WSL; elsewhere it is the same code over a
// socket.

// e2eAgentAddress returns where a test's agent listens: a named pipe of its own
// on Windows, a socket in dir everywhere else.
func e2eAgentAddress(t *testing.T, dir string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return `\\.\pipe\sshush-e2e-` + randomSuffix(t)
	}
	return filepath.Join(dir, "agent.sock")
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// relayProcess is a running "sshush relay".
type relayProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.Reader
	stderr *bytes.Buffer
	exited chan error
}

func startRelayProcess(t *testing.T, binDir, configPath, runtimeDir string) *relayProcess {
	t.Helper()
	cmd := sshushCommand(binDir, configPath, runtimeDir, "relay")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	p := &relayProcess{cmd: cmd, stdin: stdin, stdout: stdout, stderr: &bytes.Buffer{}, exited: make(chan error, 1)}
	cmd.Stderr = p.stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start relay: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return p
}

// agentStream is the relay's stdin and stdout as the one stream an agent
// client expects.
func (p *relayProcess) agentStream() io.ReadWriter {
	return struct {
		io.Reader
		io.Writer
	}{p.stdout, p.stdin}
}

// within fails the test if step, a round trip through the relay, takes longer
// than a relay that works ever would. The streams are a process's pipes, which
// have no deadline of their own, and a relay that carries one direction only
// would otherwise hang the test until the whole suite timed out.
func (p *relayProcess) within(t *testing.T, what string, step func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- step() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s: %v\nstderr: %s", what, err, p.stderr)
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("%s: no answer through the relay after 15s\nstderr: %s", what, p.stderr)
	}
}

// waitForExit waits for the relay to exit and fails the test if it does not.
// Call it once, after everything has been read from stdout.
func (p *relayProcess) waitForExit(t *testing.T) error {
	t.Helper()
	go func() { p.exited <- p.cmd.Wait() }()
	select {
	case err := <-p.exited:
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("relay still running 10s after it should have exited\nstderr: %s", p.stderr)
		return nil
	}
}

func startAgentWithKey(t *testing.T, comment string) (binDir, configPath, dir string) {
	t.Helper()
	dir = e2eWorkDir(t)
	binDir = buildBins(t)
	keyPath := writeE2ETestKey(t, dir, "id_ed25519", comment)
	configPath = writeE2EConfig(t, dir, e2eAgentAddress(t, dir), "", []string{keyPath})
	t.Cleanup(func() { runSSHush(t, binDir, configPath, dir, nil, "stop") })
	if _, stderr, code := runSSHush(t, binDir, configPath, dir, nil, "start"); code != 0 {
		t.Fatalf("start: exit %d\nstderr: %s", code, stderr)
	}
	return binDir, configPath, dir
}

func TestE2E_RelayCarriesTheAgentOverStdio(t *testing.T) {
	binDir, configPath, dir := startAgentWithKey(t, "e2e-relay-key")

	relay := startRelayProcess(t, binDir, configPath, dir)
	client := sshagent.NewClient(relay.agentStream())

	var keys []*sshagent.Key
	relay.within(t, "list through the relay", func() (err error) {
		keys, err = client.List()
		return err
	})
	if len(keys) != 1 || keys[0].Comment != "e2e-relay-key" {
		t.Fatalf("want the one key the agent holds, got %v", keys)
	}
	data := []byte("signed through the relay")
	var sig *ssh.Signature
	relay.within(t, "sign through the relay", func() (err error) {
		sig, err = client.Sign(keys[0], data)
		return err
	})
	if err := keys[0].Verify(data, sig); err != nil {
		t.Fatalf("verify signature: %v", err)
	}
}

func TestE2E_RelayExitsWhenItsInputCloses(t *testing.T) {
	binDir, configPath, dir := startAgentWithKey(t, "e2e-relay-key")

	relay := startRelayProcess(t, binDir, configPath, dir)
	// One round trip first, so the relay is known to be connected to the agent
	// rather than still starting when its input goes away.
	relay.within(t, "list through the relay", func() error {
		_, err := sshagent.NewClient(relay.agentStream()).List()
		return err
	})
	if err := relay.stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := relay.waitForExit(t); err != nil {
		t.Fatalf("relay should exit cleanly once its input closes: %v\nstderr: %s", err, relay.stderr)
	}
}

func TestE2E_RelayWithNoAgentSaysSoOnStderr(t *testing.T) {
	dir := e2eWorkDir(t)
	binDir := buildBins(t)
	// A valid config for an agent that is never started.
	keyPath := writeE2ETestKey(t, dir, "id_ed25519", "e2e-relay-key")
	configPath := writeE2EConfig(t, dir, e2eAgentAddress(t, dir), "", []string{keyPath})

	stdout, stderr, code := runSSHush(t, binDir, configPath, dir, nil, "relay")
	if code == 0 {
		t.Fatalf("relay with no agent should fail; stderr: %s", stderr)
	}
	// Stdout is the protocol stream: anything written there would reach the
	// client as a malformed agent reply.
	if stdout != "" {
		t.Errorf("relay must leave stdout to the protocol, got %q", stdout)
	}
	if !strings.Contains(stderr, "no agent is running") {
		t.Errorf("stderr should say no agent is running, got %q", stderr)
	}
}
