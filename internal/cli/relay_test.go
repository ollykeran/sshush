package cli

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ollykeran/sshush/internal/agent"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// stdio joins a reader and a writer into the stream an agent client talks over.
type stdio struct {
	io.Reader
	io.Writer
}

// TestRelay_carriesTheAgentProtocolOverStdio drives 'sshush relay' the way the
// sshush in WSL does: the agent protocol in on stdin, out on stdout.
func TestRelay_carriesTheAgentProtocolOverStdio(t *testing.T) {
	socketPath, direct := startTestAgent(t)
	keyPath := writeTestKey(t, t.TempDir(), "id_ed25519", "through-the-relay")
	if err := agent.AddKeyFromPath(direct, keyPath); err != nil {
		t.Fatalf("seed key: %v", err)
	}

	toRelay, clientWrites := io.Pipe()
	clientReads, fromRelay := io.Pipe()
	relayDone := make(chan error, 1)
	go func() { relayDone <- relayAgent(socketPath, toRelay, fromRelay) }()

	client := sshagent.NewClient(stdio{clientReads, clientWrites})
	keys, err := client.List()
	if err != nil {
		t.Fatalf("list through the relay: %v", err)
	}
	if len(keys) != 1 || keys[0].Comment != "through-the-relay" {
		t.Fatalf("keys through the relay: want through-the-relay, got %v", keys)
	}

	// Stdin closing is the client hanging up, and ends the relay.
	_ = clientWrites.Close()
	select {
	case err := <-relayDone:
		if err != nil {
			t.Fatalf("relay: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("relay did not end when its stdin closed")
	}
}

// TestRelay_noAgentSaysSo: the far side reads this off stderr to explain why
// nothing answers, so it has to name the problem and the fix.
func TestRelay_noAgentSaysSo(t *testing.T) {
	t.Parallel()
	err := relayAgent(filepath.Join(t.TempDir(), "no-agent-here.sock"), strings.NewReader(""), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no agent is running") {
		t.Fatalf("want an error saying no agent is running, got: %v", err)
	}
}
