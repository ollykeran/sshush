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

	env := f.env()
	env.AgentVaultPath = filepath.Join(f.dir, "some-other-vault.json")
	if _, err := List(env); err != nil {
		t.Fatalf("list: %v", err)
	}
	if f.ask.calls != 0 {
		t.Fatalf("passphrase prompts: want 0, got %d", f.ask.calls)
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
