package sshushd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/config"
)

// TestRunDaemonOnly_refusesAnExternalAgent: the socket of an agent sshush does
// not own must never get a daemon of ours listening on it, whoever asks. The
// refusal comes before anything is detached or written.
func TestRunDaemonOnly_refusesAnExternalAgent(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "sshush.pid")
	cfg := config.Config{AgentType: config.AgentTypeExternal, SocketPath: filepath.Join(dir, "someone-elses.sock")}

	err := RunDaemonOnly(cfg, pidFile, nil)
	if err == nil || !strings.Contains(err.Error(), "external") {
		t.Fatalf("want a refusal naming external mode, got: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "*")); len(matches) != 0 {
		t.Fatalf("want nothing written, found: %v", matches)
	}
}

func TestAgentDaemonSupported_ownAgents(t *testing.T) {
	for _, agentType := range []string{config.AgentTypeKeys, config.AgentTypeVault} {
		if err := agentDaemonSupported(config.Config{AgentType: agentType}); err != nil {
			t.Errorf("type %q: want it served, got: %v", agentType, err)
		}
	}
}
