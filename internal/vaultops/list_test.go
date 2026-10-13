package vaultops

import (
	"path/filepath"
	"testing"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/vault"
)

func TestList_noVaultPathIsAnError(t *testing.T) {
	_, err := List(Env{})
	if got := CodeOf(err); got != CodeNoVaultPath {
		t.Fatalf("code: want %v, got %v (err %v)", CodeNoVaultPath, got, err)
	}
}

// TestList_uninitializedVaultIsNotAnError is the seam's half of the TUI
// regression guard: the pre-init state is a state, not a failure, and the front
// end decides how to invite the user to create the vault.
func TestList_uninitializedVaultIsNotAnError(t *testing.T) {
	dir := unixSocketTempDirVaultOps(t)
	res, err := List(Env{VaultPath: filepath.Join(dir, "vault.json")})
	if err != nil {
		t.Fatalf("list: want no error, got %v", err)
	}
	if res.Initialized {
		t.Fatal("initialized: want false, got true")
	}
}

func TestList_reportsLoadStateFromAgent(t *testing.T) {
	f := startVaultAgent(t, false)
	_, fingerprint := f.addKey(t, "id_loaded", "loaded-key", true)

	res, err := List(f.env())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !res.AgentReachable {
		t.Fatal("agent reachable: want true, got false")
	}
	if len(res.Identities) != 1 {
		t.Fatalf("identities: want 1, got %d", len(res.Identities))
	}
	if got := res.Identities[0].Loaded; got != LoadYes {
		t.Fatalf("loaded: want %v, got %v", LoadYes, got)
	}

	session, err := agent.Open(f.socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := vault.SessionUnload(session, fingerprint); err != nil {
		t.Fatalf("session unload: %v", err)
	}

	res, err = List(f.env())
	if err != nil {
		t.Fatalf("list after unload: %v", err)
	}
	if got := res.Identities[0].Loaded; got != LoadNo {
		t.Fatalf("loaded after unload: want %v, got %v", LoadNo, got)
	}
}

func TestList_noAgentReportsLoadUnknown(t *testing.T) {
	f := startVaultAgent(t, false)
	f.addKey(t, "id_orphan", "orphan-key", true)

	env := f.env()
	env.SocketPath = filepath.Join(f.dir, "nothing-here.sock")
	res, err := List(env)
	if err != nil {
		t.Fatalf("list: want no error without an agent, got %v", err)
	}
	if res.AgentReachable {
		t.Fatal("agent reachable: want false, got true")
	}
	if got := res.Identities[0].Loaded; got != LoadUnknown {
		t.Fatalf("loaded: want %v, got %v", LoadUnknown, got)
	}
}

// TestList_emptyVaultDoesNotContactAgent guards the one behaviour that is easy
// to lose in the move: listing a vault with nothing in it used to return before
// the socket was touched, so it never prompted. Attaching the agent
// unconditionally would ask for a passphrase nobody needs to give.
func TestList_emptyVaultDoesNotContactAgent(t *testing.T) {
	f := startVaultAgent(t, false)
	f.lock(t)

	res, err := List(f.env())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !res.Initialized || len(res.Identities) != 0 {
		t.Fatalf("want an initialized empty vault, got initialized=%v %d identities", res.Initialized, len(res.Identities))
	}
	if f.ask.calls != 0 {
		t.Fatalf("passphrase prompts: want 0, got %d", f.ask.calls)
	}
}

func TestList_unlocksLockedAgentServingThisVault(t *testing.T) {
	f := startVaultAgent(t, false)
	f.addKey(t, "id_autoload", "autoload-key", true)
	f.lock(t)

	res, err := List(f.env())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if f.ask.calls != 1 {
		t.Fatalf("passphrase prompts: want 1, got %d", f.ask.calls)
	}
	if got := res.Identities[0].Loaded; got != LoadYes {
		t.Fatalf("loaded after transparent unlock: want %v, got %v", LoadYes, got)
	}
}

// TestList_doesNotPromptForAnAgentServingAnotherVault keeps the guard the CLI
// had: a passphrase is only ever asked for on an agent that serves the vault
// being operated on, so --vault-path elsewhere never fishes for the running
// agent's secret.
func TestList_doesNotPromptForAnAgentServingAnotherVault(t *testing.T) {
	f := startVaultAgent(t, false)
	f.addKey(t, "id_autoload", "autoload-key", true)
	f.lock(t)

	env := f.namedEnv(f.vaultPath)
	env.AgentVaultPath = filepath.Join(f.dir, "some-other-vault.json")
	res, err := List(env)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(res.Identities) != 1 {
		t.Fatalf("identities: want the named vault's 1, got %d", len(res.Identities))
	}
	if f.ask.calls != 0 {
		t.Fatalf("passphrase prompts: want 0, got %d", f.ask.calls)
	}
}

// TestList_namedVaultIsReadFromItsFile: --vault-path means that file, even
// with a vault agent running that would happily answer for its own.
func TestList_namedVaultIsReadFromItsFile(t *testing.T) {
	f := startVaultAgent(t, false)
	f.addKey(t, "id_agent", "in-the-agents-vault", true)

	other := filepath.Join(f.dir, "other", "vault.json")
	store, err := vault.Open(other)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(store, []byte("another-passphrase")); err != nil {
		t.Fatal(err)
	}

	res, err := List(f.namedEnv(other))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !res.Initialized || len(res.Identities) != 0 {
		t.Fatalf("want the named vault, initialized and empty, got initialized=%v %d identities", res.Initialized, len(res.Identities))
	}
	if res.VaultPath != other {
		t.Fatalf("vault path: want %s, got %s", other, res.VaultPath)
	}
}

// TestList_withoutAVaultFile is the shared-daemon case: the client has no
// vault path at all, and the agent answers for its own vault, including the
// identities it is not serving this session.
func TestList_withoutAVaultFile(t *testing.T) {
	f := startVaultAgent(t, false)
	f.addKey(t, "id_loaded", "loaded-key", true)
	f.addKey(t, "id_stored", "stored-key", false)
	session, err := agent.Open(f.socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	stored := f.store.AllIdentities()[1]
	if err := vault.SessionUnload(session, stored.Fingerprint); err != nil {
		t.Fatalf("session unload: %v", err)
	}

	res, err := List(f.remoteEnv())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !res.Initialized || !res.AgentReachable {
		t.Fatalf("want initialized and reachable, got initialized=%v reachable=%v", res.Initialized, res.AgentReachable)
	}
	if res.VaultPath != f.vaultPath {
		t.Fatalf("vault path: want the agent's %s, got %s", f.vaultPath, res.VaultPath)
	}
	if len(res.Identities) != 2 {
		t.Fatalf("identities: want 2, got %d", len(res.Identities))
	}
	want := map[string]LoadState{"loaded-key": LoadYes, "stored-key": LoadNo}
	for _, id := range res.Identities {
		if id.Loaded != want[id.Comment] {
			t.Errorf("%s loaded: want %v, got %v", id.Comment, want[id.Comment], id.Loaded)
		}
		if id.KeyType != "ssh-ed25519" || id.Filepath == "" {
			t.Errorf("%s: want key type and file path carried over, got %q %q", id.Comment, id.KeyType, id.Filepath)
		}
	}
	if f.ask.calls != 0 {
		t.Fatalf("passphrase prompts on an unlocked vault: want 0, got %d", f.ask.calls)
	}
}

// TestList_unlocksLockedAgentWithoutAVaultFile: a locked vault tells its
// socket nothing, so with no file to read the passphrase is the only way to an
// answer, and it is asked for once.
func TestList_unlocksLockedAgentWithoutAVaultFile(t *testing.T) {
	f := startVaultAgent(t, false)
	f.addKey(t, "id_autoload", "autoload-key", true)
	f.lock(t)

	res, err := List(f.remoteEnv())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if f.ask.calls != 1 {
		t.Fatalf("passphrase prompts: want 1, got %d", f.ask.calls)
	}
	if len(res.Identities) != 1 || res.Identities[0].Loaded != LoadYes {
		t.Fatalf("want the one identity, loaded, got %+v", res.Identities)
	}
}

// TestList_lockedAgentWithoutAVaultFileOrAPromptReportsLocked is what the TUI
// sees in that case: it cannot prompt from a tea.Cmd, so it is told the vault
// is locked and offers its own unlock.
func TestList_lockedAgentWithoutAVaultFileOrAPromptReportsLocked(t *testing.T) {
	f := startVaultAgent(t, false)
	f.addKey(t, "id_autoload", "autoload-key", true)
	f.lock(t)

	_, err := List(Env{SocketPath: f.socketPath})
	if got := CodeOf(err); got != CodeVaultLocked {
		t.Fatalf("code: want %v, got %v (err %v)", CodeVaultLocked, got, err)
	}
}

// TestList_legacyAgentFallsBackToTheVaultFile keeps a new sshush working
// against a daemon that was running before the upgrade.
func TestList_legacyAgentFallsBackToTheVaultFile(t *testing.T) {
	f := startLegacyVaultAgent(t)
	f.addKey(t, "id_loaded", "loaded-key", true)

	res, err := List(f.env())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(res.Identities) != 1 || res.Identities[0].Loaded != LoadYes {
		t.Fatalf("want the one identity, loaded, got %+v", res.Identities)
	}
	if res.VaultPath != f.vaultPath {
		t.Fatalf("vault path: want %s, got %s", f.vaultPath, res.VaultPath)
	}
}

// TestList_unmanagedAgentThatIsNotThereIsTheProblem: with no vault path and an
// agent someone else runs, "set vault_path" is the wrong advice.
func TestList_unmanagedAgentThatIsNotThereIsTheProblem(t *testing.T) {
	dir := unixSocketTempDirVaultOps(t)

	_, err := List(Env{SocketPath: filepath.Join(dir, "nothing-here.sock"), AgentUnmanaged: true})
	if got := CodeOf(err); got != CodeNoAgent {
		t.Fatalf("code: want %v, got %v (err %v)", CodeNoAgent, got, err)
	}
}

// TestList_doesNotPromptWhenAskPassphraseIsNil is the TUI's contract: it cannot
// block for input inside a tea.Cmd, so the seam must never try.
func TestList_doesNotPromptWhenAskPassphraseIsNil(t *testing.T) {
	f := startVaultAgent(t, false)
	f.addKey(t, "id_autoload", "autoload-key", true)
	f.lock(t)

	res, err := List(f.silentEnv())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := res.Identities[0].Loaded; got != LoadNo {
		t.Fatalf("loaded on a locked agent: want %v, got %v", LoadNo, got)
	}
	if f.ask.calls != 0 {
		t.Fatalf("passphrase prompts: want 0, got %d", f.ask.calls)
	}
}
