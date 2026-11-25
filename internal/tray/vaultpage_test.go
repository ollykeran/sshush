package tray

import (
	"strings"
	"testing"
	"time"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/vaultops"
)

// rowValue returns the value of the row labelled label, and whether there is one.
func rowValue(v VaultView, label string) (string, bool) {
	for _, r := range v.Rows {
		if r.Label == label {
			return r.Value, true
		}
	}
	return "", false
}

func TestVaultOf(t *testing.T) {
	created := time.Date(2026, 3, 1, 9, 30, 0, 0, time.Local)
	full := vaultUp()
	full.Listed.AgentReachable = true
	full.Listed.Created = created
	full.Listed.Recovery = vaultops.RecoveryOn
	full.Listed.Identities = []vaultops.IdentityView{
		{Fingerprint: "SHA256:a", Loaded: vaultops.LoadYes},
		{Fingerprint: "SHA256:b", Loaded: vaultops.LoadNo},
	}
	full.FileFound, full.FileSize, full.FileModTime = true, 2048, created

	v := vaultOf(full)
	if v.Title != "Vault · unlocked" || v.Path != "/v/vault.json" {
		t.Errorf("title %q path %q", v.Title, v.Path)
	}
	want := map[string]string{
		"Size":            "2.0 KB",
		"Last changed":    "1 Mar 2026, 09:30",
		"Created":         "1 Mar 2026, 09:30",
		"Keys":            "2 keys, 1 loaded",
		"Recovery phrase": "Set up: the phrase can unlock the vault",
	}
	for label, value := range want {
		if got, ok := rowValue(v, label); !ok || got != value {
			t.Errorf("%s: got %q (present %v), want %q", label, got, ok, value)
		}
	}
	if _, ok := rowValue(v, "Location"); !ok {
		t.Error("no Location row")
	}

	// What the window could not learn is left out, not shown as blank.
	sparse := vaultUp()
	v = vaultOf(sparse)
	for _, label := range []string{"Size", "Last changed", "Created"} {
		if _, ok := rowValue(v, label); ok {
			t.Errorf("sparse: unexpected %s row", label)
		}
	}
	if got, _ := rowValue(v, "Recovery phrase"); !strings.HasPrefix(got, "Unknown") {
		t.Errorf("sparse recovery: got %q", got)
	}
}

func TestVaultOfStates(t *testing.T) {
	locked := vaultUp()
	locked.Backend.VaultLocked = true
	down := MainState{Managed: true, Listing: true, Listed: vaultops.ListResult{Initialized: true, VaultPath: "/v/vault.json"},
		ConfigIsVault: true}
	noVault := MainState{Managed: true, Reachable: true, Backend: agent.Backend{Mode: "vault"}, Listing: true,
		Listed: vaultops.ListResult{VaultPath: "/v/vault.json"}, ConfigIsVault: true}
	keys := MainState{Managed: true, Reachable: true, Backend: agent.Backend{Mode: "keys"}, ConfiguredVault: "/v/vault.json"}
	broken := MainState{Managed: true, Listing: true, ListErr: &vaultops.OpError{Code: vaultops.CodeVaultUnreadable, Msg: "open vault: bad"},
		ConfiguredVault: "/v/vault.json", ConfigIsVault: true}
	unmanaged := MainState{Reachable: true, Backend: agent.Backend{Mode: "keys"}}
	onDisk := vaultUp()
	onDisk.FileFound = true

	tests := []struct {
		name      string
		state     MainState
		title     string
		sub       string
		rows      bool // more than the location
		path      string
		actions   VaultActions
		hasLocate bool
	}{
		{"unlocked", vaultUp(), "Vault · unlocked", "available", true, "/v/vault.json",
			VaultActions{Lock: true, OpenFolder: true, CopyPath: true, Import: true, UseKeys: true}, true},
		{"unlocked, file on this PC", onDisk, "Vault · unlocked", "available", true, "/v/vault.json",
			VaultActions{Lock: true, OpenFolder: true, CopyPath: true, Backup: true, Import: true, UseKeys: true}, true},
		{"locked", locked, "Vault · locked", "Unlock", true, "/v/vault.json",
			VaultActions{Unlock: true, OpenFolder: true, CopyPath: true, Import: true, UseKeys: true}, true},
		{"agent down", down, "Vault · agent stopped", "Start the agent", true, "/v/vault.json",
			VaultActions{OpenFolder: true, CopyPath: true, Import: true, UseKeys: true}, true},
		{"no vault yet", noVault, "No vault yet", "Create one", false, "/v/vault.json",
			VaultActions{CreateVault: true, OpenFolder: true, CopyPath: true, Import: true, UseKeys: true}, true},
		{"key files", keys, "Not using a vault", "key files", false, "/v/vault.json",
			VaultActions{OpenFolder: true, CopyPath: true, Import: true, UseVault: true}, true},
		{"key files, no vault configured", MainState{Managed: true, Reachable: true, Backend: agent.Backend{Mode: "keys"}},
			"Not using a vault", "key files", false, "", VaultActions{Import: true, UseVault: true}, false},
		{"unmanaged agent", unmanaged, "Not using a vault", "key files", false, "", VaultActions{}, false},
		{"unreadable", broken, "Vault", "open vault: bad", false, "/v/vault.json",
			VaultActions{OpenFolder: true, CopyPath: true, Import: true, UseKeys: true}, true},
	}
	for _, tc := range tests {
		v := vaultOf(tc.state)
		if v.Title != tc.title {
			t.Errorf("%s: title %q, want %q", tc.name, v.Title, tc.title)
		}
		if !strings.Contains(v.Sub, tc.sub) {
			t.Errorf("%s: sub %q, want it to mention %q", tc.name, v.Sub, tc.sub)
		}
		if v.Path != tc.path {
			t.Errorf("%s: path %q, want %q", tc.name, v.Path, tc.path)
		}
		if _, ok := rowValue(v, "Location"); ok != tc.hasLocate {
			t.Errorf("%s: location row %v, want %v", tc.name, ok, tc.hasLocate)
		}
		if got := len(v.Rows) > 1; got != tc.rows {
			t.Errorf("%s: rows %+v", tc.name, v.Rows)
		}
		if got := vaultActions(tc.state); got != tc.actions {
			t.Errorf("%s: actions %+v, want %+v", tc.name, got, tc.actions)
		}
	}
}

func TestByteSize(t *testing.T) {
	for n, want := range map[int64]string{1: "1 byte", 900: "900 bytes", 1536: "1.5 KB", 3 << 20: "3.0 MB"} {
		if got := byteSize(n); got != want {
			t.Errorf("byteSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestBackupName(t *testing.T) {
	if got := backupName(time.Date(2026, 11, 5, 23, 0, 0, 0, time.Local)); got != "vault-20261105.json" {
		t.Errorf("got %q", got)
	}
}

func TestImportQuestion(t *testing.T) {
	sum := vaultops.FileSummary{Keys: 3, Recovery: true}
	q := importQuestion("/u/laptop.json", sum, "/v/vault.json", false)
	if !strings.Contains(q, "laptop.json") || !strings.Contains(q, "3 keys") || strings.Contains(q, "kept") {
		t.Errorf("fresh: %q", q)
	}
	if q := importQuestion("/u/laptop.json", sum, "/v/vault.json", true); !strings.Contains(q, "vault.json.bak-") {
		t.Errorf("replacing: %q", q)
	}
	st := importedStatus(vaultops.ImportResult{FileSummary: sum, Previous: "/v/vault.json.bak-20261125-101500"})
	if !strings.Contains(st, "vault.json.bak-20261125-101500") {
		t.Errorf("status %q", st)
	}
}

func TestSwitchQuestion(t *testing.T) {
	if q := switchQuestion(true, false); !strings.Contains(q, "Create the vault") {
		t.Errorf("to a new vault: %q", q)
	}
	if q := switchQuestion(true, true); !strings.Contains(q, "passphrase") {
		t.Errorf("to a vault: %q", q)
	}
	if q := switchQuestion(false, true); !strings.Contains(q, "key files") {
		t.Errorf("to keys: %q", q)
	}
}
