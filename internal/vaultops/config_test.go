package vaultops

import (
	"testing"

	"github.com/ollykeran/sshush/internal/config"
)

// TestEnvFromConfig pins what each agent type tells the vault verbs: a vault
// agent answers for its own vault, an external one is not sshush's to manage,
// and both it and the Windows agent WSL relays to run where 'sshush reload'
// here does not reach.
func TestEnvFromConfig(t *testing.T) {
	t.Parallel()
	const sock = "/tmp/a.sock"
	cases := []struct {
		name string
		cfg  *config.Config
		want Env
	}{
		{name: "no config", want: Env{SocketPath: sock}},
		{
			name: "vault agent",
			cfg:  &config.Config{AgentType: config.AgentTypeVault, VaultPath: "/v/vault.json"},
			want: Env{SocketPath: sock, VaultPath: "/v/vault.json", AgentVaultPath: "/v/vault.json"},
		},
		{
			name: "keys agent with a vault file",
			cfg:  &config.Config{AgentType: config.AgentTypeKeys, VaultPath: "/v/vault.json"},
			want: Env{SocketPath: sock, VaultPath: "/v/vault.json"},
		},
		{
			name: "external agent",
			cfg:  &config.Config{AgentType: config.AgentTypeExternal},
			want: Env{SocketPath: sock, AgentUnmanaged: true, AgentElsewhere: true},
		},
		{
			name: "the Windows agent from WSL",
			cfg:  &config.Config{AgentType: config.AgentTypeWindows},
			want: Env{SocketPath: sock, AgentElsewhere: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EnvFromConfig(tc.cfg, sock)
			if got.AskPassphrase != nil {
				t.Error("AskPassphrase is the front end's to set")
			}
			if got.VaultPath != tc.want.VaultPath || got.NamedVault || got.SocketPath != tc.want.SocketPath ||
				got.AgentVaultPath != tc.want.AgentVaultPath || got.AgentUnmanaged != tc.want.AgentUnmanaged ||
				got.AgentElsewhere != tc.want.AgentElsewhere {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
