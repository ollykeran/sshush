package tray

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"runtime"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/vaultops"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func TestMainTemplateLayout(t *testing.T) {
	b := mainTemplate()
	title, offsets, ids, classes, texts, names := readTemplateNames(t, b)
	if title != "sshush" {
		t.Errorf("title: got %q", title)
	}
	style := uint32(b[0]) | uint32(b[1])<<16
	if style&wsThickFrame == 0 || style&wsMaximizeBox == 0 {
		t.Error("the main window cannot be resized or maximized")
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
		if (id == idMainFp || id == idMainPub) && uint32(b[offsets[i]])&esReadOnly == 0 {
			t.Errorf("field %d can be edited", id)
		}
	}
	// Every control the layout places is in the template, and the reverse.
	placed := mainLayout(1000, 700, layoutMetrics{dpi: 96})
	for id := range placed {
		if !seen[id] {
			t.Errorf("the layout places %d, which the template lacks", id)
		}
	}
	for id := range seen {
		if _, ok := placed[id]; !ok {
			t.Errorf("control %d is never placed", id)
		}
	}
	// Each keyboard mnemonic reaches one control.
	keys := map[rune]string{}
	for _, text := range append(texts, autoloadLabel(true), autoloadLabel(false), HeaderLock{Locked: true}.label(), "&Start agent", "&Unload") {
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
	pairs := [][2]string{{"&Lock", "Unlo&ck…"}, {autoloadLabel(true), autoloadLabel(false)}, {"S&top agent", "&Start agent"}, {"L&oad", "&Unload"}}
	for _, p := range pairs {
		if (a == p[0] && b == p[1]) || (a == p[1] && b == p[0]) {
			return true
		}
	}
	return false
}

func vaultUp() MainState {
	return MainState{Managed: true, Reachable: true, Backend: agent.Backend{Mode: "vault", SpeaksOps: true},
		Listing: true, Listed: vaultops.ListResult{Initialized: true, VaultPath: "/v/vault.json"}, ConfigIsVault: true}
}

func TestMainActions(t *testing.T) {
	pub := []byte("a public key")
	loaded := &vaultops.IdentityView{Fingerprint: "SHA256:a", Loaded: vaultops.LoadYes, Autoload: true, PublicKey: pub}
	unloaded := &vaultops.IdentityView{Fingerprint: "SHA256:b", Loaded: vaultops.LoadNo, PublicKey: pub}
	unparseable := &vaultops.IdentityView{Loaded: vaultops.LoadYes}
	noVault := MainState{Managed: true, Reachable: true, Backend: agent.Backend{Mode: "vault", SpeaksOps: true}, Listing: true}
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
		{"vault, loaded selected", vaultUp(), loaded, MainActions{Add: true, Lock: true, Stop: true, Remove: true, Unload: true, Autoload: true, Rename: true, Copy: true}},
		{"vault, unloaded selected", vaultUp(), unloaded, MainActions{Add: true, Lock: true, Stop: true, Remove: true, Load: true, Autoload: true, AutoloadOn: true, Rename: true, Copy: true}},
		{"locked", locked, unloaded, MainActions{Unlock: true, Stop: true, Copy: true}},
		{"agent down", down, unloaded, MainActions{Start: true, Copy: true}},
		{"no vault yet", noVault, nil, MainActions{CreateVault: true, Stop: true}},
		{"no vault yet, agent down", MainState{Managed: true, Listing: true}, nil, MainActions{CreateVault: true}},
		{"keyring", keyring, loaded, MainActions{Add: true, Stop: true, Unload: true, Rename: true, Copy: true}},
		{"keyring, unparseable key", keyring, unparseable, MainActions{Add: true, Stop: true}},
		{"external agent", external, loaded, MainActions{Add: true, Unload: true, Copy: true}},
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
		{"no vault yet, agent down", MainState{Managed: true, Listing: true}, "Create one", false},
		{"no vault yet", MainState{Managed: true, Reachable: true, Backend: agent.Backend{Mode: "vault"}, Listing: true}, "Create one", false},
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
	k := testKey(t, "")
	id := vaultops.IdentityView{Fingerprint: "SHA256:x", KeyType: k.Format, Loaded: vaultops.LoadYes, Autoload: true, PublicKey: k.Blob}
	tests := []struct {
		inVault bool
		want    []string
	}{
		{true, []string{"ED25519 key", "ED25519", "Loaded · autoload"}},
		{false, []string{"ED25519 key", "ED25519", "Loaded"}},
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

func TestKeyTypeLabel(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaPub, _ := ssh.NewPublicKey(&rsaKey.PublicKey)
	ecKey, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	ecPub, _ := ssh.NewPublicKey(&ecKey.PublicKey)
	tests := []struct {
		id   vaultops.IdentityView
		want string
	}{
		{vaultops.IdentityView{PublicKey: testKey(t, "").Blob}, "ED25519"},
		{vaultops.IdentityView{PublicKey: rsaPub.Marshal()}, "RSA 2048"},
		{vaultops.IdentityView{PublicKey: ecPub.Marshal()}, "ECDSA P-384"},
		{vaultops.IdentityView{KeyType: "ssh-foo"}, "ssh-foo"},
		{vaultops.IdentityView{KeyType: "?"}, "Unknown"},
	}
	for _, tc := range tests {
		if got := keyTypeLabel(tc.id); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}

func TestDetailOf(t *testing.T) {
	k := testKey(t, "")
	id := vaultops.IdentityView{Fingerprint: "SHA256:x", Comment: "work", Loaded: vaultops.LoadNo, PublicKey: k.Blob, Filepath: "/keys/id_work"}
	v := vaultUp()
	v.Listed.Identities = []vaultops.IdentityView{id}

	d := detailOf(v, &id)
	if d.Empty || d.Title != "work" || d.Fingerprint != "SHA256:x" {
		t.Errorf("vault key: got %+v", d)
	}
	if !strings.HasPrefix(d.PublicKey, "ssh-ed25519 AAAA") || !strings.HasSuffix(d.PublicKey, " work") {
		t.Errorf("public key line: got %q", d.PublicKey)
	}
	if d.Sub != "ED25519 · not loaded · loads only when you ask" {
		t.Errorf("sub: got %q", d.Sub)
	}
	if !strings.HasSuffix(d.From, "id_work") {
		t.Errorf("from: got %q", d.From)
	}

	wsl := id
	wsl.Filepath = `\\wsl.localhost\Debian\home\olly\.ssh\id_rsa`
	if d := detailOf(v, &wsl); d.From != "WSL (Debian) · ~/.ssh/id_rsa" || d.FromReveal != wsl.Filepath {
		t.Errorf("key from WSL: got %q, reveal %q", d.From, d.FromReveal)
	}
	win := id
	win.Filepath = `D:\keys\id_rsa`
	if d := detailOf(v, &win); d.From != `Windows · D:\keys\id_rsa` || d.FromReveal != win.Filepath {
		t.Errorf("key from Windows: got %q, reveal %q", d.From, d.FromReveal)
	}
	legacy := id
	legacy.Filepath = "/home/olly/.ssh/id_rsa"
	if d := detailOf(v, &legacy); runtime.GOOS == "windows" {
		if d.From != "WSL · ~/.ssh/id_rsa" || d.FromReveal != "" || d.FromLinux != legacy.Filepath {
			t.Errorf("key from WSL, distro not recorded: got %+v", d)
		}
		d = d.withDistro("Ubuntu")
		if d.From != "WSL (Ubuntu) · ~/.ssh/id_rsa" || d.FromReveal != `\\wsl.localhost\Ubuntu\home\olly\.ssh\id_rsa` || d.FromLinux != "" {
			t.Errorf("after finding the distro: got %+v", d)
		}
	}
	if d := detailOf(v, &vaultops.IdentityView{Fingerprint: "SHA256:y", PublicKey: k.Blob}); d.FromReveal != "" {
		t.Errorf("no recorded path should have nothing to show: got %+v", d)
	}

	if d := detailOf(v, nil); !d.Empty || d.Sub == "" {
		t.Errorf("nothing selected among keys: got %+v", d)
	}
	if d := detailOf(MainState{}, nil); !d.Empty || d.Sub != "" {
		t.Errorf("nothing to select: got %+v", d)
	}
	bad := vaultops.IdentityView{Comment: "odd"}
	if d := detailOf(v, &bad); d.PublicKey != "" || d.Fingerprint == "" {
		t.Errorf("unparseable key: got %+v", d)
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

func TestRestartReason(t *testing.T) {
	keys := MainState{Managed: true, Reachable: true, Backend: agent.Backend{Mode: "keys"}}
	if r := restartReason(keys, false); r != "" {
		t.Errorf("nothing changed: %q", r)
	}
	if r := restartReason(keys, true); !strings.Contains(r, "Restart") {
		t.Errorf("settings saved: %q", r)
	}
	toVault := keys
	toVault.ConfigIsVault = true
	if r := restartReason(toVault, false); !strings.Contains(r, "vault") {
		t.Errorf("config now serves the vault: %q", r)
	}
	toKeys := vaultUp()
	toKeys.ConfigIsVault = false
	if r := restartReason(toKeys, false); !strings.Contains(r, "key files") {
		t.Errorf("config now serves key files: %q", r)
	}
	stopped := keys
	stopped.Reachable = false
	if r := restartReason(stopped, true); r != "" || canRestart(stopped) {
		t.Errorf("a stopped agent starts with the settings as they are: %q", r)
	}
	unmanaged := keys
	unmanaged.Managed = false
	if canRestart(unmanaged) || restartReason(unmanaged, true) != "" {
		t.Error("an agent sshush does not run is not restarted from here")
	}
}
