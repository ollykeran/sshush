package tray

import (
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/vaultops"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func TestMainTemplateLayout(t *testing.T) {
	b := mainTemplate()
	title, offsets, ids, classes, texts, names := readTemplateNames(t, b)
	if title != "sshush" {
		t.Errorf("title: got %q", title)
	}
	seen := map[uint16]bool{}
	for i, id := range ids {
		if seen[id] {
			t.Errorf("item %d: id %d used twice", i, id)
		}
		seen[id] = true
		if offsets[i]%2 != 0 {
			t.Errorf("item %d starts at word %d, not on a DWORD boundary", i, offsets[i])
		}
		if id == idMainList && (names[i] != classListView || classes[i] != 0) {
			t.Errorf("list: class %#x name %q, want %q", classes[i], names[i], classListView)
		}
		if id == idMainTheme && classes[i] != classComboBox {
			t.Errorf("theme: class %#x, want a combo box", classes[i])
		}
	}
	for _, id := range []uint16{idMainAgent, idMainAgentToggle, idMainLock, idMainSource, idMainList, idMainStatus,
		idMainAdd, idMainRemove, idMainLoad, idMainUnload, idMainAutoload, idMainSelftest, idMainThemeLabel,
		idMainTheme, idMainRefresh, idCancel} {
		if !seen[id] {
			t.Errorf("no item with id %d", id)
		}
	}
	// Every item lies inside the window.
	width, height := int16(b[7]), int16(b[8])
	for i, off := range offsets {
		x, y, cx, cy := int16(b[off+4]), int16(b[off+5]), int16(b[off+6]), int16(b[off+7])
		if ids[i] == idMainTheme {
			cy = 14 // the rest is its drop-down
		}
		if x < 0 || y < 0 || x+cx > width || y+cy > height {
			t.Errorf("item %d (id %d) at %d,%d %dx%d falls outside the %dx%d window", i, ids[i], x, y, cx, cy, width, height)
		}
	}
	// Each keyboard mnemonic reaches one control.
	keys := map[rune]string{}
	for _, text := range append(texts, autoloadLabel(true), autoloadLabel(false), "Unlo&ck…", "&Start agent") {
		i := strings.IndexRune(text, '&')
		if i < 0 || i+1 >= len(text) {
			continue
		}
		k := []rune(strings.ToLower(text[i+1:]))[0]
		if other, ok := keys[k]; ok && other != text && !alternates(other, text) {
			t.Errorf("mnemonic %q is on both %q and %q", k, other, text)
		}
		keys[k] = text
	}
}

// alternates reports whether a and b are labels one button switches between.
func alternates(a, b string) bool {
	pairs := [][2]string{{"&Lock", "Unlo&ck…"}, {autoloadLabel(true), autoloadLabel(false)}, {"S&top agent", "&Start agent"}}
	for _, p := range pairs {
		if (a == p[0] && b == p[1]) || (a == p[1] && b == p[0]) {
			return true
		}
	}
	return false
}

func vaultUp() MainState {
	return MainState{Managed: true, Reachable: true, Backend: agent.Backend{Mode: "vault", SpeaksOps: true},
		Listing: true, Listed: vaultops.ListResult{Initialized: true, VaultPath: "/v/vault.json"}}
}

func TestMainActions(t *testing.T) {
	loaded := &vaultops.IdentityView{Fingerprint: "SHA256:a", Loaded: vaultops.LoadYes, Autoload: true}
	unloaded := &vaultops.IdentityView{Fingerprint: "SHA256:b", Loaded: vaultops.LoadNo}
	unparseable := &vaultops.IdentityView{Loaded: vaultops.LoadYes}
	locked := vaultUp()
	locked.Backend.VaultLocked = true
	down := MainState{Managed: true, Listing: true, Listed: vaultops.ListResult{Initialized: true}}
	keyring := MainState{Managed: true, Reachable: true, Backend: agent.Backend{Mode: "keys", SpeaksOps: true}}
	external := MainState{Reachable: true, Backend: agent.Backend{Mode: "keys"}}

	tests := []struct {
		name  string
		state MainState
		sel   *vaultops.IdentityView
		want  MainActions
	}{
		{"vault, nothing selected", vaultUp(), nil, MainActions{Add: true, Lock: true, Stop: true}},
		{"vault, loaded selected", vaultUp(), loaded, MainActions{Add: true, Lock: true, Stop: true, Remove: true, Unload: true, Autoload: true}},
		{"vault, unloaded selected", vaultUp(), unloaded, MainActions{Add: true, Lock: true, Stop: true, Remove: true, Load: true, Autoload: true, AutoloadOn: true}},
		{"locked", locked, unloaded, MainActions{Unlock: true, Stop: true}},
		{"agent down", down, unloaded, MainActions{Start: true}},
		{"keyring", keyring, loaded, MainActions{Add: true, Stop: true, Unload: true}},
		{"keyring, unparseable key", keyring, unparseable, MainActions{Add: true, Stop: true}},
		{"external agent", external, loaded, MainActions{Add: true, Unload: true}},
		{"external agent down", MainState{}, nil, MainActions{}},
	}
	for _, tc := range tests {
		if got := mainActions(tc.state, tc.sel); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestMainRows(t *testing.T) {
	v := vaultUp()
	v.Listed.Identities = []vaultops.IdentityView{{Fingerprint: "SHA256:v"}}
	if rows := v.Rows(); len(rows) != 1 || rows[0].Fingerprint != "SHA256:v" {
		t.Errorf("vault rows: got %+v", rows)
	}
	k := MainState{Reachable: true, Backend: agent.Backend{Mode: "keys"}, Keys: []*sshagent.Key{testKey(t, "mem")}}
	rows := k.Rows()
	if len(rows) != 1 || rows[0].Comment != "mem" || rows[0].Loaded != vaultops.LoadYes || !strings.HasPrefix(rows[0].Fingerprint, "SHA256:") {
		t.Errorf("keyring rows: got %+v", rows)
	}
	locked := vaultUp()
	locked.Backend.VaultLocked = true
	locked.ListErr = &vaultops.OpError{Code: vaultops.CodeVaultLocked, Msg: "vault is locked"}
	if rows := locked.Rows(); len(rows) != 0 {
		t.Errorf("locked vault with nothing to read: got %+v", rows)
	}
}

func TestMainStatus(t *testing.T) {
	withIDs := func(s MainState, n int) MainState {
		s.Listed.Identities = make([]vaultops.IdentityView, n)
		return s
	}
	locked := vaultUp()
	locked.Backend.VaultLocked = true
	lockedNoFile := locked
	lockedNoFile.ListErr = &vaultops.OpError{Code: vaultops.CodeVaultLocked, Msg: "vault is locked"}
	failed := vaultUp()
	failed.ListErr = &vaultops.OpError{Code: vaultops.CodeLocalIO, Msg: "read vault", Hint: "Check the file."}
	down := MainState{Managed: true, Listing: true, Listed: vaultops.ListResult{Initialized: true}}

	tests := []struct {
		name    string
		state   MainState
		want    string
		wantErr bool
	}{
		{"one identity", withIDs(vaultUp(), 1), "1 identity in the vault.", false},
		{"several", withIDs(vaultUp(), 3), "3 identities in the vault.", false},
		{"agent down, vault file read", withIDs(down, 2), "Start the agent", false},
		{"agent down, no vault", MainState{Managed: true}, "not running", false},
		{"external agent down", MainState{}, "not reachable", true},
		{"locked, file read", withIDs(locked, 2), "unlock it", false},
		{"locked, nothing to read", lockedNoFile, "The vault is locked.", false},
		{"no vault yet", MainState{Managed: true, Reachable: true, Backend: agent.Backend{Mode: "vault"}, Listing: true}, "sshush vault init", false},
		{"keyring", MainState{Reachable: true, Backend: agent.Backend{Mode: "keys"}, Keys: make([]*sshagent.Key, 2)}, "2 keys in the agent.", false},
		{"list failed", failed, "read vault. Check the file.", true},
	}
	for _, tc := range tests {
		got, isErr := mainStatus(tc.state)
		if !strings.Contains(got, tc.want) || isErr != tc.wantErr {
			t.Errorf("%s: got %q (error %v), want it to contain %q (error %v)", tc.name, got, isErr, tc.want, tc.wantErr)
		}
	}
}

func TestKeyRow(t *testing.T) {
	id := vaultops.IdentityView{Fingerprint: "SHA256:x", KeyType: "ssh-ed25519", Loaded: vaultops.LoadYes, Autoload: true}
	tests := []struct {
		inVault bool
		want    []string
	}{
		{true, []string{"(no comment)", "ssh-ed25519", "SHA256:x", "yes", "yes", "-"}},
		{false, []string{"(no comment)", "ssh-ed25519", "SHA256:x", "yes", "-", "-"}},
	}
	for _, tc := range tests {
		row := keyRow(id, tc.inVault)
		if len(row) != len(mainColumns) {
			t.Fatalf("row has %d cells for %d columns", len(row), len(mainColumns))
		}
		for i := range tc.want {
			if row[i] != tc.want[i] {
				t.Errorf("in vault %v, cell %d (%s): got %q, want %q", tc.inVault, i, mainColumns[i], row[i], tc.want[i])
			}
		}
	}
}

func TestSourceLine(t *testing.T) {
	if got := sourceLine(vaultUp()); !strings.HasPrefix(got, "Vault: ") || !strings.HasSuffix(got, "vault.json") {
		t.Errorf("vault: got %q", got)
	}
	if got := sourceLine(MainState{Reachable: true, Backend: agent.Backend{Mode: "keys"}}); got != "Keys in the agent's memory" {
		t.Errorf("keyring: got %q", got)
	}
	if got := sourceLine(MainState{}); got != "" {
		t.Errorf("nothing: got %q", got)
	}
}

func TestRowsPutUnloadedKeysLast(t *testing.T) {
	v := vaultUp()
	v.Listed.Identities = []vaultops.IdentityView{
		{Fingerprint: "a", Loaded: vaultops.LoadNo},
		{Fingerprint: "b", Loaded: vaultops.LoadYes},
		{Fingerprint: "c", Loaded: vaultops.LoadUnknown},
		{Fingerprint: "d", Loaded: vaultops.LoadYes},
		{Fingerprint: "e", Loaded: vaultops.LoadNo},
	}
	var got []string
	for _, r := range v.Rows() {
		got = append(got, r.Fingerprint)
	}
	if strings.Join(got, "") != "bdcae" {
		t.Errorf("order: got %v, want loaded first, keeping listed order within each group", got)
	}
	if v.Listed.Identities[0].Fingerprint != "a" {
		t.Error("Rows reordered the listing it was given")
	}
	for _, r := range v.Rows() {
		if Dimmed(r) != (r.Loaded != vaultops.LoadYes) {
			t.Errorf("%s: dimmed %v with loaded %v", r.Fingerprint, Dimmed(r), r.Loaded)
		}
	}
}

func TestHeaderOf(t *testing.T) {
	locked := vaultUp()
	locked.Backend.VaultLocked = true
	tests := []struct {
		name  string
		state MainState
		tone  Tone
		icon  Icon
	}{
		{"unlocked vault", vaultUp(), ToneGood, IconUnlocked},
		{"locked vault", locked, ToneWarn, IconLocked},
		{"sshush keyring", MainState{Reachable: true, Backend: agent.Backend{Mode: "keys", SpeaksOps: true}}, ToneGood, IconKeys},
		{"external agent", MainState{Reachable: true, Backend: agent.Backend{Mode: "keys"}}, ToneNeutral, IconKeys},
		{"not running", MainState{Managed: true}, ToneBad, IconUnreachable},
		{"not reachable", MainState{}, ToneBad, IconUnreachable},
	}
	for _, tc := range tests {
		if tone, icon := headerOf(tc.state); tone != tc.tone || icon != tc.icon {
			t.Errorf("%s: got tone %d icon %d, want %d %d", tc.name, tone, icon, tc.tone, tc.icon)
		}
	}
}

func TestButtonRowsCoverTheTemplate(t *testing.T) {
	_, _, ids, _, _, _ := readTemplateNames(t, mainTemplate())
	inTemplate := map[uint16]bool{}
	for _, id := range ids {
		inTemplate[id] = true
	}
	seen := map[uint16]bool{}
	for _, row := range buttonRows {
		for _, id := range row.ids {
			if !inTemplate[id] {
				t.Errorf("row control %d is not in the template", id)
			}
			if seen[id] {
				t.Errorf("control %d is in two rows", id)
			}
			seen[id] = true
		}
	}
	for id := range buttonGlyphs {
		if !seen[id] {
			t.Errorf("button %d has an icon but is in no row, so is never sized for it", id)
		}
	}
}
