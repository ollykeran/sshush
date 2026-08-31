package cli

import (
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/spf13/cobra"
)

func TestUnlockRecovery_RequiresVaultAgent(t *testing.T) {
	// startTestAgent is a plain keyring (keys-mode), same shape as a real
	// foreign ssh-agent: no vault extensions supported.
	socketPath, _ := startTestAgent(t)

	orig := env.Config
	env.Config = &config.Config{SocketPath: socketPath}
	t.Cleanup(func() { env.Config = orig })

	cmd := &cobra.Command{}
	err := runUnlockRecovery(cmd, nil)
	if err == nil {
		t.Fatal("expected runUnlockRecovery to error against a non-vault agent")
	}
	if !strings.Contains(err.Error(), "requires a running vault agent") {
		t.Errorf("expected error to mention 'requires a running vault agent', got: %v", err)
	}
}
