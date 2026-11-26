package e2e

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/kbolino/pageant"
	sshagent "golang.org/x/crypto/ssh/agent"

	sshushpageant "github.com/ollykeran/sshush/internal/pageant"
)

// A PuTTY-family program (MobaXterm with "Use external Pageant", WinSCP) finds
// the running sshushd as Pageant and uses the key it holds.
func TestE2E_PageantClientUsesTheAgentsKeys(t *testing.T) {
	if _, taken := sshushpageant.OwnerPID(); taken {
		t.Skip("another program already answers as Pageant on this machine")
	}
	dir := e2eWorkDir(t)
	binDir := buildBins(t)
	keyPath := writeE2ETestKey(t, dir, "id_ed25519", "e2e-pageant-key")
	configPath := writeE2EConfig(t, dir, e2eAgentAddress(t, dir), "", []string{keyPath})
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	on := strings.Replace(string(data), "pageant_emulation = false", "pageant_emulation = true", 1)
	if err := os.WriteFile(configPath, []byte(on), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runSSHush(t, binDir, configPath, dir, nil, "stop") })
	if _, stderr, code := runSSHush(t, binDir, configPath, dir, nil, "start"); code != 0 {
		t.Fatalf("start: exit %d\nstderr: %s", code, stderr)
	}

	conn, err := pageant.NewConn()
	if err != nil {
		t.Fatalf("no Pageant to connect to: %v", err)
	}
	defer conn.Close()
	// Given an io.Closer, the agent client pipelines: it reads before it has
	// written. A Pageant Conn is one request then its reply, so it is handed
	// over as a plain reader and writer.
	client := sshagent.NewClient(struct {
		io.Reader
		io.Writer
	}{conn, conn})
	keys, err := client.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 || keys[0].Comment != "e2e-pageant-key" {
		t.Fatalf("want the one key the agent holds, got %v", keys)
	}
	msg := []byte("signed through Pageant")
	sig, err := client.Sign(keys[0], msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := keys[0].Verify(msg, sig); err != nil {
		t.Fatalf("verify: %v", err)
	}
}
