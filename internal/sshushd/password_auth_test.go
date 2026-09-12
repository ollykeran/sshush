package sshushd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/vault"
)

func TestPasswordAuthVault_NeedsAVaultPath(t *testing.T) {
	_, err := passwordAuthVault(config.Config{ServerPasswordAuth: true})
	if err == nil || !strings.Contains(err.Error(), "vault_path") {
		t.Errorf("error = %v, want one pointing at [vault].vault_path", err)
	}
}

func TestPasswordAuthVault_RefusesAMissingVault(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "vault.json")
	if _, err := passwordAuthVault(config.Config{VaultPath: missing}); err == nil {
		t.Error("a missing vault was accepted")
	}
}

func TestPasswordAuthVault_RefusesAnUninitializedVault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	if err := os.WriteFile(path, []byte(`{"version": 1, "identities": []}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := passwordAuthVault(config.Config{VaultPath: path})
	if err == nil || !strings.Contains(err.Error(), "vault init") {
		t.Errorf("error = %v, want one saying to run vault init", err)
	}
}

func TestPasswordAuthVault_ResolvesAVaultDirectory(t *testing.T) {
	dir := t.TempDir()
	store, err := vault.Open(filepath.Join(dir, "vault.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(store, []byte("e2epassthatalwaysmeetspasswordrequirementsA1!")); err != nil {
		t.Fatal(err)
	}

	got, err := passwordAuthVault(config.Config{VaultPath: dir})
	if err != nil {
		t.Fatalf("passwordAuthVault: %v", err)
	}
	if want := filepath.Join(dir, "vault.json"); got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}
