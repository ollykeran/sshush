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
			VaultActions{Lock: true, OpenFolder: true, CopyPath: true, Import: true, ChangePassphrase: true, NewRecovery: true}, true},
		{"unlocked, file on this PC", onDisk, "Vault · unlocked", "available", true, "/v/vault.json",
			VaultActions{Lock: true, OpenFolder: true, CopyPath: true, Backup: true, Import: true, ChangePassphrase: true, NewRecovery: true}, true},
		{"locked", locked, "Vault · locked", "Unlock", true, "/v/vault.json",
			VaultActions{Unlock: true, OpenFolder: true, CopyPath: true, Import: true, ChangePassphrase: true, NewRecovery: true}, true},
		{"agent down", down, "Vault · agent stopped", "Start the agent", true, "/v/vault.json",
			VaultActions{OpenFolder: true, CopyPath: true, Import: true, ChangePassphrase: true, NewRecovery: true}, true},
		{"no vault yet", noVault, "No vault yet", "Create one", false, "/v/vault.json",
			VaultActions{CreateVault: true, OpenFolder: true, CopyPath: true, Import: true}, true},
		{"key files", keys, "Not using a vault", "key files", false, "/v/vault.json",
			VaultActions{OpenFolder: true, CopyPath: true, Import: true}, true},
		{"key files, no vault configured", MainState{Managed: true, Reachable: true, Backend: agent.Backend{Mode: "keys"}},
			"Not using a vault", "key files", false, "", VaultActions{Import: true}, false},
		{"unmanaged agent", unmanaged, "Not using a vault", "key files", false, "", VaultActions{}, false},
		{"unreadable", broken, "Vault", "open vault: bad", false, "/v/vault.json",
			VaultActions{OpenFolder: true, CopyPath: true, Import: true}, true},
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

func TestReplacedRecoveryMessage(t *testing.T) {
	if m := replacedRecoveryMessage(""); !strings.Contains(m, "old one no longer") || strings.Contains(m, "copy") {
		t.Errorf("no file: %q", m)
	}
	if m := replacedRecoveryMessage("/v/recovery.txt"); !strings.Contains(m, "recovery.txt") {
		t.Errorf("file: %q", m)
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

// keysAside is an agent serving key files, with the vault read from its file.
func keysAside(initialized bool) MainState {
	return MainState{Managed: true, Reachable: true, Backend: agent.Backend{Mode: "keys", SpeaksOps: true},
		Keys: nil, Listing: true, VaultAside: true, VaultFile: "/v/vault.json",
		Listed: vaultops.ListResult{Initialized: initialized, VaultPath: "/v/vault.json"}}
}

func TestVaultPageInKeysMode(t *testing.T) {
	none := keysAside(false)
	v, va := vaultOf(none), vaultActions(none)
	if v.Title != "No vault yet" || !strings.Contains(v.Sub, "key files") || v.Path != "/v/vault.json" {
		t.Errorf("no vault: %q / %q / %q", v.Title, v.Sub, v.Path)
	}
	if !va.CreateVault || va.ChangePassphrase || va.Backup {
		t.Errorf("no vault: actions %+v", va)
	}
	if h := headerLock(none); h.Shown {
		t.Errorf("no vault: the header offers to lock key files: %+v", h)
	}

	have := keysAside(true)
	have.FileFound = true
	v, va = vaultOf(have), vaultActions(have)
	if v.Title != "Vault · not in use" || len(v.Rows) < 2 {
		t.Errorf("vault aside: %q, rows %+v", v.Title, v.Rows)
	}
	if va.CreateVault || !va.ChangePassphrase || !va.NewRecovery || !va.Backup || va.Lock || va.Unlock {
		t.Errorf("vault aside: actions %+v", va)
	}
	if h := headerLock(have); h.Shown || h.Enabled {
		t.Errorf("vault aside: header lock %+v", h)
	}
	if c := v.Captions[idAutoLockCaption]; !strings.Contains(c, "once the agent serves") {
		t.Errorf("auto-lock caption %q", c)
	}
}

func TestKeysPageIgnoresAVaultAside(t *testing.T) {
	s := keysAside(true)
	s.Listed.Identities = []vaultops.IdentityView{{Fingerprint: "SHA256:v"}}
	if s.showsVault() || s.needsVault() || len(s.Rows()) != 0 {
		t.Errorf("the Keys page shows the vault aside: %+v", s.Rows())
	}
	if text, _ := mainStatus(s); !strings.Contains(text, "0 keys in the agent") {
		t.Errorf("status %q", text)
	}
	broken := keysAside(false)
	broken.ListErr = &vaultops.OpError{Code: vaultops.CodeVaultUnreadable, Msg: "open vault: bad"}
	if text, isErr := mainStatus(broken); isErr || strings.Contains(text, "bad") {
		t.Errorf("a broken vault aside shows on the Keys page: %q", text)
	}
	if v := vaultOf(broken); !strings.Contains(v.Sub, "bad") || v.Path != "/v/vault.json" {
		t.Errorf("but not on the Vault page: %+v", v)
	}
}

func TestHeaderLock(t *testing.T) {
	locked := vaultUp()
	locked.Backend.VaultLocked = true
	stopped := MainState{Managed: true, Listing: true, Listed: vaultops.ListResult{Initialized: true}, ConfigIsVault: true}
	for name, tc := range map[string]struct {
		s     MainState
		want  HeaderLock
		label string
	}{
		"unlocked": {vaultUp(), HeaderLock{Shown: true, Enabled: true}, "&Lock vault"},
		"locked":   {locked, HeaderLock{Shown: true, Enabled: true, Locked: true}, "Unlo&ck vault…"},
		// It starts locked: the button says so, greyed until it runs.
		"stopped": {stopped, HeaderLock{Shown: true, Locked: true}, "Unlo&ck vault…"},
	} {
		got := headerLock(tc.s)
		if got != tc.want || got.label() != tc.label {
			t.Errorf("%s: got %+v %q, want %+v %q", name, got, got.label(), tc.want, tc.label)
		}
	}
}
