package cli

import (
	"testing"

	"github.com/ollykeran/sshush/internal/config"
)

func TestEffectiveTUIMode(t *testing.T) {
	cases := []struct {
		name          string
		cfg           *config.Config
		wantMode      string
		wantVaultPath string
	}{
		{"nil config", nil, "keys", ""},
		{
			"external agent",
			&config.Config{AgentType: config.AgentTypeExternal, VaultPath: "/v.json"},
			"keys", "",
		},
		{
			"vault agent",
			&config.Config{AgentType: config.AgentTypeVault, VaultPath: "/v.json"},
			"vault", "/v.json",
		},
		{
			"keys agent",
			&config.Config{AgentType: config.AgentTypeKeys, KeyPaths: []string{"/k"}},
			"keys", "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode, vaultPath := effectiveTUIMode(tc.cfg)
			if mode != tc.wantMode {
				t.Errorf("mode: got %q, want %q", mode, tc.wantMode)
			}
			if vaultPath != tc.wantVaultPath {
				t.Errorf("vaultPath: got %q, want %q", vaultPath, tc.wantVaultPath)
			}
		})
	}
}
