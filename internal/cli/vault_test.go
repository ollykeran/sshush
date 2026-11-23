package cli

import (
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/config"
)

func TestUnlockRecovery_RequiresVaultAgent(t *testing.T) {
	t.Parallel()
	// startTestAgent is a plain keyring (keys-mode), same shape as a real
	// foreign ssh-agent: no vault extensions supported.
	socketPath, _ := startTestAgent(t)

	cmd := newConfiguredCommand(&config.Config{SocketPath: socketPath})
	err := runUnlockRecovery(cmd, nil)
	if err == nil {
		t.Fatal("expected runUnlockRecovery to error against a non-vault agent")
	}
	if !strings.Contains(err.Error(), "requires a running vault agent") {
		t.Errorf("expected error to mention 'requires a running vault agent', got: %v", err)
	}
}

// TestVaultEnv_followsTheAgentUnlessAVaultIsNamed pins what the vault verbs are
// told about the vault. Config's vault_path is only a fallback for when no
// vault agent answers; --vault-path is the user naming a file, which wins; and
// an external agent is one sshush must not tell the user to start or reload.
func TestVaultEnv_followsTheAgentUnlessAVaultIsNamed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		cfg            config.Config
		flag           string
		wantVaultPath  string
		wantNamed      bool
		wantAgentVault string
		wantUnmanaged  bool
	}{
		{
			name:           "vault config",
			cfg:            config.Config{SocketPath: "/tmp/a.sock", AgentType: config.AgentTypeVault, VaultPath: "/v/vault.json"},
			wantVaultPath:  "/v/vault.json",
			wantAgentVault: "/v/vault.json",
		},
		{
			name:           "vault config with another vault named",
			cfg:            config.Config{SocketPath: "/tmp/a.sock", AgentType: config.AgentTypeVault, VaultPath: "/v/vault.json"},
			flag:           "/elsewhere/vault.json",
			wantVaultPath:  "/elsewhere/vault.json",
			wantNamed:      true,
			wantAgentVault: "/v/vault.json",
		},
		{
			name:          "external agent with no vault file here",
			cfg:           config.Config{SocketPath: "/tmp/a.sock", AgentType: config.AgentTypeExternal},
			wantUnmanaged: true,
		},
		{
			name:          "keys agent with a vault file for the vault commands",
			cfg:           config.Config{SocketPath: "/tmp/a.sock", AgentType: config.AgentTypeKeys, VaultPath: "/v/vault.json"},
			wantVaultPath: "/v/vault.json",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newVaultListCommand()
			withConfig(cmd, &tc.cfg)
			if tc.flag != "" {
				if err := cmd.Flags().Set("vault-path", tc.flag); err != nil {
					t.Fatal(err)
				}
			}
			env, err := vaultEnv(cmd)
			if err != nil {
				t.Fatalf("vaultEnv: %v", err)
			}
			if env.VaultPath != tc.wantVaultPath || env.NamedVault != tc.wantNamed {
				t.Errorf("vault: want path %q named %v, got path %q named %v", tc.wantVaultPath, tc.wantNamed, env.VaultPath, env.NamedVault)
			}
			if env.AgentVaultPath != tc.wantAgentVault {
				t.Errorf("agent vault path: want %q, got %q", tc.wantAgentVault, env.AgentVaultPath)
			}
			if env.AgentUnmanaged != tc.wantUnmanaged {
				t.Errorf("agent unmanaged: want %v, got %v", tc.wantUnmanaged, env.AgentUnmanaged)
			}
			if env.AskPassphrase == nil {
				t.Error("ask passphrase: the CLI can prompt, so it must be set")
			}
		})
	}
}
