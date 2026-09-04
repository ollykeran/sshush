package tui

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/theme"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// lockTestAgent locks the agent at socketPath, failing the test if it cannot.
func lockTestAgent(t *testing.T, socketPath string) {
	t.Helper()
	session, err := agent.Open(socketPath)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	defer session.Close()
	if err := session.Lock(nil); err != nil {
		t.Fatalf("lock: %v", err)
	}
}

// startVaultAgentWithPath is like startTestVaultAgentTUI but also returns the on-disk
// vault path, needed by list/remove/autoload commands that take a vault path directly.
func startVaultAgentWithPath(t *testing.T, passphrase []byte) (socketPath, vaultPath string, store *vault.VaultStore) {
	t.Helper()
	dir := unixSocketTempDirTUI(t)
	socketPath = filepath.Join(dir, "agent.sock")
	vaultPath = filepath.Join(dir, "vault.json")

	var err error
	store, err = vault.Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(store, passphrase); err != nil {
		t.Fatal(err)
	}
	va := vault.NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
	})
	go func() {
		_ = agent.ListenAndServe(ctx, socketPath, va)
	}()

	var conn net.Conn
	for i := 0; i < 50; i++ {
		conn, err = net.Dial("unix", socketPath)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial agent socket: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	_ = sshagent.NewClient(conn)
	return socketPath, vaultPath, store
}

func newVaultTestSkeleton(vaultPath, socketPath string) (*Skeleton, *VaultScreen) {
	sk := NewSkeleton()
	sk.theme = theme.DefaultTheme()
	sk.styles = BuildStyles(sk.theme)
	vs := NewVaultScreen(sk, "", socketPath, vaultPath)
	sk.AddPage("vault", "Vault", vs)
	sk.activeTab = 0
	sk.navFocus = navFocusScreen
	return sk, vs
}

// seedVaultRows fills the screen the way a vaultIdentitiesMsg would, so the
// table's cells always match the columns its width affords.
func seedVaultRows(vs *VaultScreen, n int) {
	rows := make([]vaultIdentityRow, n)
	for i := 0; i < n; i++ {
		rows[i] = vaultIdentityRow{
			fingerprint: "SHA256:fp" + string(rune('0'+i)),
			loaded:      "no",
			comment:     "key",
			keyType:     "ssh-ed25519",
			keyFile:     "~/.ssh/id_ed25519",
		}
	}
	vs.rows = rows
	vs.resizeTable()
}

func TestNewTUIVaultTabRegisteredOnlyInVaultMode(t *testing.T) {
	keysMode := NewTUI("", "/tmp/agent.sock", theme.DefaultTheme(), "keys", "")
	for _, p := range keysMode.pages {
		if p.id == "vault" {
			t.Fatal("keys mode should not register a vault tab")
		}
	}

	vaultMode := NewTUI("", "/tmp/agent.sock", theme.DefaultTheme(), "vault", "/tmp/vault.json")
	found := false
	for _, p := range vaultMode.pages {
		if p.id == "vault" {
			found = true
		}
	}
	if !found {
		t.Fatal("vault mode should register a vault tab")
	}
	if _, ok := vaultMode.pages[0].model.(*AgentScreen); !ok {
		t.Fatal("Agent must remain page 0 even when vault tab is registered")
	}
}

func TestVaultScreenHasModal(t *testing.T) {
	_, vs := newVaultTestSkeleton("/tmp/vault.json", "/tmp/agent.sock")
	if vs.HasModal() {
		t.Fatal("expected no modal open initially")
	}
	vs.startInit()
	if !vs.HasModal() {
		t.Fatal("expected HasModal true while passphrase prompt open")
	}
	if !vs.HasActiveTextInput() {
		t.Fatal("expected HasActiveTextInput true while passphrase prompt open")
	}
}

func TestVaultTableCursorMovesDownAndUp(t *testing.T) {
	_, vs := newVaultTestSkeleton("/tmp/vault.json", "/tmp/agent.sock")
	seedVaultRows(vs, 3)
	vs.table.SetCursor(0)

	_, _ = vs.Update(tea.KeyPressMsg{Code: 'j'})
	if vs.table.Cursor() != 1 {
		t.Fatalf("cursor=%d, want 1 after j", vs.table.Cursor())
	}

	_, _ = vs.Update(tea.KeyPressMsg{Code: 'k'})
	if vs.table.Cursor() != 0 {
		t.Fatalf("cursor=%d, want 0 after k", vs.table.Cursor())
	}
}

func TestVaultTableUpAtFirstRowNavigatesToTabs(t *testing.T) {
	sk, vs := newVaultTestSkeleton("/tmp/vault.json", "/tmp/agent.sock")
	seedVaultRows(vs, 2)
	vs.table.SetCursor(0)

	_, cmd := vs.Update(tea.KeyPressMsg{Code: 'k'})
	if cmd == nil {
		t.Fatal("expected navToTabBar cmd")
	}
	msg := cmd()
	if _, ok := msg.(NavToTabBarMsg); !ok {
		t.Fatalf("cmd msg type=%T, want NavToTabBarMsg", msg)
	}
	updated, _ := sk.Update(msg)
	sk = updated.(*Skeleton)
	if sk.navFocus != navFocusTabs {
		t.Fatalf("navFocus=%v, want navFocusTabs", sk.navFocus)
	}
}

func TestVaultInitAlreadyExistsErrors(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := vault.Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(store, []byte("existing")); err != nil {
		t.Fatal(err)
	}

	cmd := initVaultCmd(vaultPath, []byte("new-pass"), true)
	msg := cmd()
	result, ok := msg.(vaultInitResultMsg)
	if !ok {
		t.Fatalf("expected vaultInitResultMsg, got %T", msg)
	}
	if result.err == nil {
		t.Fatal("expected error for already-initialized vault")
	}
}

func TestVaultInitSuccessGeneratesRecovery(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")

	cmd := initVaultCmd(vaultPath, []byte("brand-new-pass"), true)
	msg := cmd()
	result, ok := msg.(vaultInitResultMsg)
	if !ok {
		t.Fatalf("expected vaultInitResultMsg, got %T", msg)
	}
	if result.err != nil {
		t.Fatalf("unexpected init error: %v", result.err)
	}
	if result.mnemonic == "" {
		t.Fatal("expected a recovery mnemonic when withRecovery=true")
	}
	if result.recoveryFile == "" {
		t.Fatal("expected a recovery.txt path")
	}

	store, err := vault.Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if store.GetMetadata() == nil {
		t.Fatal("expected initialized vault metadata")
	}
}

// TestVaultListIdentitiesNotInitializedIsNotAnError covers the not-yet-initialized
// state: it must not be reported as an error (that would show as a scary status line
// and, before this fix, made the Init button impossible to gate on correctly) — it's
// the normal pre-init condition, signaled via initialized=false.
func TestVaultListIdentitiesNotInitializedIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")

	cmd := listVaultIdentitiesCmd(vaultPath, "/tmp/does-not-exist.sock")
	msg := cmd()
	result, ok := msg.(vaultIdentitiesMsg)
	if !ok {
		t.Fatalf("expected vaultIdentitiesMsg, got %T", msg)
	}
	if result.err != nil {
		t.Fatalf("expected no error for uninitialized vault, got %v", result.err)
	}
	if result.initialized {
		t.Fatal("expected initialized=false for a vault path with no vault.json yet")
	}
}

func TestVaultAddListRemoveAutoloadRoundtrip(t *testing.T) {
	socketPath, vaultPath, _ := startVaultAgentWithPath(t, []byte("roundtrip-pass"))
	dir := t.TempDir()
	privPath, fp := writeTUITestKey(t, dir, "id_ed25519", "roundtrip-key")

	// Add with autoload on.
	addMsg := addVaultKeyCmd(socketPath, privPath, true)().(vaultOpResultMsg)
	if addMsg.err != nil {
		t.Fatalf("add: %v", addMsg.err)
	}

	listMsg := listVaultIdentitiesCmd(vaultPath, socketPath)().(vaultIdentitiesMsg)
	if listMsg.err != nil {
		t.Fatalf("list: %v", listMsg.err)
	}
	if len(listMsg.rows) != 1 {
		t.Fatalf("expected 1 identity, got %d", len(listMsg.rows))
	}
	row := listMsg.rows[0]
	if row.fingerprint != fp {
		t.Errorf("fingerprint: got %q, want %q", row.fingerprint, fp)
	}
	if row.loaded != "yes" {
		t.Errorf("loaded: got %q, want yes", row.loaded)
	}
	if !row.autoload {
		t.Error("expected autoload true")
	}
	if row.comment != "roundtrip-key" {
		t.Errorf("comment: got %q, want roundtrip-key", row.comment)
	}
	if row.keyFile != utils.DisplayPath(privPath) {
		t.Errorf("key file: got %q, want %q", row.keyFile, utils.DisplayPath(privPath))
	}

	// Turn autoload off.
	autoMsg := setVaultAutoloadCmd(socketPath, fp, false)().(vaultOpResultMsg)
	if autoMsg.err != nil {
		t.Fatalf("autoload off: %v", autoMsg.err)
	}
	listMsg = listVaultIdentitiesCmd(vaultPath, socketPath)().(vaultIdentitiesMsg)
	if listMsg.rows[0].autoload {
		t.Error("expected autoload false after toggle")
	}

	// Remove.
	removeMsg := removeVaultIdentityCmd(socketPath, vaultPath, fp)().(vaultOpResultMsg)
	if removeMsg.err != nil {
		t.Fatalf("remove: %v", removeMsg.err)
	}
	listMsg = listVaultIdentitiesCmd(vaultPath, socketPath)().(vaultIdentitiesMsg)
	if len(listMsg.rows) != 0 {
		t.Fatalf("expected 0 identities after remove, got %d", len(listMsg.rows))
	}
}

func TestVaultUnlockRecoveryFailsWithoutRecoveryEnabled(t *testing.T) {
	socketPath, _ := startTestVaultAgentTUI(t, []byte("no-recovery-pass"))
	lockTestAgent(t, socketPath)

	msg := unlockVaultRecoveryCmd(socketPath, "abandon abandon abandon")().(vaultOpResultMsg)
	if msg.err == nil {
		t.Fatal("expected recovery unlock to fail when the vault has no recovery metadata")
	}
}

// TestSwitchingToVaultTabRefreshesIdentities covers the case where the vault was
// unlocked (or populated) via the Agent tab, or the Vault tab's initial Init() ran
// before the daemon/vault was ready: switching to the Vault tab must re-list, not
// show whatever (possibly empty/errored) state Init() captured at TUI startup.
func TestSwitchingToVaultTabRefreshesIdentities(t *testing.T) {
	socketPath, vaultPath, _ := startVaultAgentWithPath(t, []byte("switch-tab-pass"))

	sk := NewSkeleton()
	sk.theme = theme.DefaultTheme()
	sk.styles = BuildStyles(sk.theme)
	agentScreen := NewAgentScreen(sk, "", socketPath)
	vs := NewVaultScreen(sk, "", socketPath, vaultPath)
	sk.AddPage("agent", "Agent", agentScreen)
	sk.AddPage("vault", "Vault", vs)
	sk.activeTab = 0
	sk.navFocus = navFocusScreen

	// Vault is empty at this point: Init() (never called here) would have listed 0 rows.
	if len(vs.table.Rows()) != 0 {
		t.Fatalf("expected 0 rows before any key is added, got %d", len(vs.table.Rows()))
	}

	// Add a key "from outside" the Vault screen (simulating another path adding to the vault).
	dir := t.TempDir()
	privPath, fp := writeTUITestKey(t, dir, "id_ed25519", "added-elsewhere")
	if err := addVaultKeyCmd(socketPath, privPath, true)().(vaultOpResultMsg).err; err != nil {
		t.Fatalf("add: %v", err)
	}

	// Switching to the Vault tab must refresh, picking up the key added above even
	// though this VaultScreen instance never received a vaultOpResultMsg for it.
	cmd := sk.switchTab(1)
	if cmd == nil {
		t.Fatal("expected a refresh cmd when switching to the Vault tab")
	}
	msg := cmd()
	var msgs []tea.Msg
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				msgs = append(msgs, c())
			}
		}
	} else {
		msgs = append(msgs, msg)
	}
	var gotIdentities bool
	for _, m := range msgs {
		if res, ok := m.(vaultIdentitiesMsg); ok {
			gotIdentities = true
			if res.err != nil {
				t.Fatalf("refresh list error: %v", res.err)
			}
			if len(res.rows) != 1 || res.rows[0].fingerprint != fp {
				t.Fatalf("expected 1 row with fingerprint %s, got %+v", fp, res.rows)
			}
			updated, _ := vs.Update(res)
			vs = updated.(*VaultScreen)
		}
	}
	if !gotIdentities {
		t.Fatalf("expected switchTab's cmd to include a vaultIdentitiesMsg refresh, got %T", msg)
	}
	if len(vs.table.Rows()) != 1 {
		t.Fatalf("expected 1 row in table after refresh, got %d", len(vs.table.Rows()))
	}
}

func TestVaultUnlockPassphraseSucceedsAfterLock(t *testing.T) {
	passphrase := []byte("lock-unlock-pass")
	socketPath, _ := startTestVaultAgentTUI(t, passphrase)
	lockTestAgent(t, socketPath)

	msg := unlockVaultPassphraseCmd(socketPath, []byte("lock-unlock-pass"))().(vaultOpResultMsg)
	if msg.err != nil {
		t.Fatalf("unlock: %v", msg.err)
	}
}

// TestVaultRowHighlightSpansAllColumns mirrors TestAgentRowHighlightSpansAllColumns:
// the selected row must get a background color across every cell, not just a leading
// cursor indicator (which read as a stray indent rather than a highlight).
func TestVaultRowHighlightSpansAllColumns(t *testing.T) {
	st := BuildStyles(theme.DefaultTheme())
	_, vs := newVaultTestSkeleton("/tmp/vault.json", "/tmp/agent.sock")
	vs.width = 120
	seedVaultRows(vs, 1)
	vs.table.SetCursor(0)

	view := vs.renderTable(st, true)
	bg := "48;2;" // lipgloss truecolor background prefix
	fpIdx := strings.Index(view, "SHA256:fp0")
	if fpIdx < 0 {
		t.Fatal("expected fingerprint in rendered table")
	}
	if strings.LastIndex(view[:fpIdx], bg) < 0 {
		t.Fatal("expected background color on the selected row's fingerprint cell")
	}
	// The row must not be rendered via bubbles/table's own cursor-indent style; a
	// leading "> " prefix on the row would be the regression this test guards against.
	for _, line := range strings.Split(view, "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, "\x1b[0123456789;m"), "> ") {
			t.Fatalf("row rendered with a cursor-indent prefix instead of a highlight: %q", line)
		}
	}
}

func TestVaultTableDownAtLastRowMovesToButtons(t *testing.T) {
	_, vs := newVaultTestSkeleton("/tmp/vault.json", "/tmp/agent.sock")
	seedVaultRows(vs, 2)
	vs.table.SetCursor(1) // last row

	_, _ = vs.Update(tea.KeyPressMsg{Code: 'j'})
	if vs.focus != vaultFocusButtons {
		t.Fatalf("focus=%d, want vaultFocusButtons after down at last row", vs.focus)
	}

	_, _ = vs.Update(tea.KeyPressMsg{Code: 'k'})
	if vs.focus != vaultFocusTable {
		t.Fatalf("focus=%d, want vaultFocusTable after up from buttons", vs.focus)
	}
}

func TestVaultButtonsLeftRightAndEnter(t *testing.T) {
	_, vs := newVaultTestSkeleton("/tmp/vault.json", "/tmp/agent.sock")
	vs.vaultInitialized = true
	vs.focus = vaultFocusButtons
	vs.buttons.Focused = true
	ids := vs.syncButtons()
	if len(ids) == 0 {
		t.Fatal("expected at least one button once initialized")
	}

	_, _ = vs.Update(tea.KeyPressMsg{Text: "right", Code: tea.KeyRight})
	if vs.buttons.Active != 1 {
		t.Fatalf("Active=%d, want 1 after right", vs.buttons.Active)
	}
	_, _ = vs.Update(tea.KeyPressMsg{Text: "left", Code: tea.KeyLeft})
	if vs.buttons.Active != 0 {
		t.Fatalf("Active=%d, want 0 after left", vs.buttons.Active)
	}
}

func TestVaultInitButtonOnlyAppearsBeforeInitialized(t *testing.T) {
	_, vs := newVaultTestSkeleton("/tmp/vault.json", "/tmp/agent.sock")

	vs.vaultInitialized = false
	descs := vs.visibleButtons()
	if len(descs) == 0 || descs[0].id != vBtnInit {
		t.Fatalf("expected Init as first button when not initialized, got %+v", descs)
	}

	vs.vaultInitialized = true
	descs = vs.visibleButtons()
	for _, d := range descs {
		if d.id == vBtnInit {
			t.Fatalf("expected no Init button once initialized, got %+v", descs)
		}
	}
}

func TestVaultUnlockLockButtonsGreyOutByLockState(t *testing.T) {
	_, vs := newVaultTestSkeleton("/tmp/vault.json", "/tmp/agent.sock")
	vs.vaultInitialized = true

	buttonByID := func(descs []vaultButtonDesc, id int) vaultButtonDesc {
		for _, d := range descs {
			if d.id == id {
				return d
			}
		}
		t.Fatalf("button id %d not found", id)
		return vaultButtonDesc{}
	}

	// Unknown lock state: neither button greyed.
	vs.sk.vaultKnown = false
	descs := vs.visibleButtons()
	if buttonByID(descs, vBtnUnlock).disabled || buttonByID(descs, vBtnLock).disabled {
		t.Fatal("expected neither Unlock nor Lock disabled when lock state is unknown")
	}

	// Known unlocked: Unlock (and Recovery) grey out, Lock stays active.
	vs.sk.vaultKnown = true
	vs.sk.vaultLocked = false
	descs = vs.visibleButtons()
	if !buttonByID(descs, vBtnUnlock).disabled {
		t.Fatal("expected Unlock disabled when vault is already unlocked")
	}
	if !buttonByID(descs, vBtnRecovery).disabled {
		t.Fatal("expected Recovery disabled when vault is already unlocked")
	}
	if buttonByID(descs, vBtnLock).disabled {
		t.Fatal("expected Lock enabled when vault is unlocked")
	}

	// Known locked: Lock greys out, Unlock stays active.
	vs.sk.vaultLocked = true
	descs = vs.visibleButtons()
	if buttonByID(descs, vBtnUnlock).disabled {
		t.Fatal("expected Unlock enabled when vault is locked")
	}
	if !buttonByID(descs, vBtnLock).disabled {
		t.Fatal("expected Lock disabled when vault is already locked")
	}
}

// TestAgentRemoveVaultKeySessionUnloadsInsteadOfDeleting covers the requested fix:
// removing a key from the Agent tab's loaded-keys table, for a vault-backed agent,
// must hide it from the running session (LOADED -> no in the Vault tab) rather than
// permanently deleting the identity from the vault. Permanent deletion stays available
// only via the Vault tab's own remove action (removeVaultIdentityCmd) and the CLI's
// `sshush vault remove`.
func TestAgentRemoveVaultKeySessionUnloadsInsteadOfDeleting(t *testing.T) {
	socketPath, vaultPath, store := startVaultAgentWithPath(t, []byte("unload-not-delete"))
	dir := t.TempDir()
	privPath, fp := writeTUITestKey(t, dir, "id_ed25519", "keep-me")

	if err := addVaultKeyCmd(socketPath, privPath, true)().(vaultOpResultMsg).err; err != nil {
		t.Fatalf("add: %v", err)
	}

	_, agentScreen := newAgentTestSkeleton()
	agentScreen.socketPath = socketPath
	agentScreen.vaultMode = "vault"
	agentScreen.keyTable.SetRows([]table.Row{{"ssh-ed25519", fp, "keep-me"}})
	agentScreen.keyTable.Table.SetCursor(0)

	_, cmd := agentScreen.removeSelectedKey()
	if cmd == nil {
		t.Fatal("expected an unload cmd")
	}
	msg := cmd()
	status, ok := msg.(agentStatusMsg)
	if !ok {
		t.Fatalf("expected agentStatusMsg, got %T", msg)
	}
	if status.isErr {
		t.Fatalf("unexpected error status: %s", status.text)
	}

	// The identity must still exist in the vault store...
	found := false
	for _, id := range store.AllIdentities() {
		if id.Fingerprint == fp {
			found = true
		}
	}
	if !found {
		t.Fatal("expected identity to remain in the vault store after Agent-tab remove")
	}

	// ...but must no longer be listed as loaded in the running agent's session.
	listMsg := listVaultIdentitiesCmd(vaultPath, socketPath)().(vaultIdentitiesMsg)
	if listMsg.err != nil {
		t.Fatalf("list: %v", listMsg.err)
	}
	if len(listMsg.rows) != 1 {
		t.Fatalf("expected 1 identity still in vault, got %d", len(listMsg.rows))
	}
	if listMsg.rows[0].loaded != "no" {
		t.Fatalf("loaded=%q, want %q after Agent-tab remove", listMsg.rows[0].loaded, "no")
	}
	if !listMsg.rows[0].autoload {
		t.Fatal("expected autoload to remain true (Agent-tab remove must not change persisted autoload)")
	}
}

// TestVaultTableShowsKeyFileWhenWide: Identity.Filepath is recorded when a key
// is added and shown by the CLI's FILEPATH column, but was invisible in the TUI.
func TestVaultTableShowsKeyFileWhenWide(t *testing.T) {
	st := BuildStyles(theme.DefaultTheme())
	_, vs := newVaultTestSkeleton("/tmp/vault.json", "/tmp/agent.sock")
	vs.width = 160
	vs.rows = []vaultIdentityRow{{
		fingerprint: "SHA256:fp0",
		loaded:      "no",
		comment:     "key",
		keyType:     "ssh-ed25519",
		keyFile:     "~/.ssh/id_ed25519",
	}}
	vs.resizeTable()

	view := vs.renderTable(st, false)
	if !strings.Contains(view, "id_ed25519") {
		t.Fatalf("expected the key file in the rendered table, got:\n%s", view)
	}
}

// TestVaultTableDropsKeyFileWhenNarrow: the column is the one worth losing when
// there is no room, and a row must not carry a cell its columns cannot index.
func TestVaultTableDropsKeyFileWhenNarrow(t *testing.T) {
	st := BuildStyles(theme.DefaultTheme())
	_, vs := newVaultTestSkeleton("/tmp/vault.json", "/tmp/agent.sock")
	vs.width = 60
	vs.rows = []vaultIdentityRow{{
		fingerprint: "SHA256:fp0",
		loaded:      "no",
		comment:     "key",
		keyType:     "ssh-ed25519",
		keyFile:     "~/.ssh/id_ed25519",
	}}
	vs.resizeTable()

	for _, col := range vs.table.Columns() {
		if col.Title == vaultKeyFileColumn {
			t.Fatal("expected the key file column to be dropped at this width")
		}
	}
	if got := len(vs.table.Rows()[0]); got != len(vs.table.Columns()) {
		t.Fatalf("row cells: want %d to match the columns, got %d", len(vs.table.Columns()), got)
	}
	// Rendering must not panic on the trimmed row.
	_ = vs.renderTable(st, false)
}

// TestVaultTableKeyFileKeepsItsFileName: a path too long for the column loses
// its leading directories, not the name that identifies it.
func TestVaultTableKeyFileKeepsItsFileName(t *testing.T) {
	st := BuildStyles(theme.DefaultTheme())
	_, vs := newVaultTestSkeleton("/tmp/vault.json", "/tmp/agent.sock")
	vs.width = 160
	vs.rows = []vaultIdentityRow{{
		fingerprint: "SHA256:fp0",
		loaded:      "no",
		comment:     "key",
		keyType:     "ssh-ed25519",
		keyFile:     "~/very/deeply/nested/directory/structure/that/will/not/fit/id_ed25519",
	}}
	vs.resizeTable()

	view := vs.renderTable(st, false)
	if !strings.Contains(view, "id_ed25519") {
		t.Fatalf("expected the file name to survive truncation, got:\n%s", view)
	}
	if strings.Contains(view, "~/very/deeply/nested/directory/structure/that/will") {
		t.Fatalf("expected the leading directories to be elided, got:\n%s", view)
	}
}

// TestVaultAddNoAutoloadRoundtrip: 'A' is the tab's --no-autoload. The key is
// stored, but with autoload off, so a daemon restart forgets it.
func TestVaultAddNoAutoloadRoundtrip(t *testing.T) {
	socketPath, vaultPath, _ := startVaultAgentWithPath(t, []byte("no-autoload-pass"))
	dir := t.TempDir()
	privPath, fp := writeTUITestKey(t, dir, "id_ed25519", "session-only-key")

	addMsg := addVaultKeyCmd(socketPath, privPath, false)().(vaultOpResultMsg)
	if addMsg.err != nil {
		t.Fatalf("add: %v", addMsg.err)
	}
	listMsg := listVaultIdentitiesCmd(vaultPath, socketPath)().(vaultIdentitiesMsg)
	if len(listMsg.rows) != 1 || listMsg.rows[0].fingerprint != fp {
		t.Fatalf("expected the key in the vault, got %+v", listMsg.rows)
	}
	if listMsg.rows[0].autoload {
		t.Error("autoload: want false after an 'A' add, got true")
	}
}

// TestVaultStartAddCarriesAutoloadChoice: the file picker round-trips through
// its own messages, so the choice made when it opened has to survive until a
// file comes back.
func TestVaultStartAddCarriesAutoloadChoice(t *testing.T) {
	_, vs := newVaultTestSkeleton("/tmp/vault.json", "/tmp/agent.sock")

	vs.handleKeys(tea.KeyPressMsg{Code: 'A', Text: "A"})
	if vs.addAutoload {
		t.Error("A: want autoload off")
	}
	if !vs.fileSelector.Visible() {
		t.Error("A: want the file picker open")
	}

	vs.handleKeys(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if !vs.addAutoload {
		t.Error("a: want autoload on")
	}
}
