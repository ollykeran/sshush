package vaultops

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/agent"
)

func TestAdd_storesKeyAndReportsBeforeAfter(t *testing.T) {
	f := startVaultAgent(t, false)
	path, fingerprint := writeTestKey(t, f.dir, "id_new", "new-key")

	res, err := Add(f.env(), []string{path}, true)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(res.Before) != 0 {
		t.Fatalf("before: want 0 keys, got %d", len(res.Before))
	}
	if len(res.After) != 1 {
		t.Fatalf("after: want 1 key, got %d", len(res.After))
	}
	if len(res.Added) != 1 || res.Added[0] != path {
		t.Fatalf("added: want [%s], got %v", path, res.Added)
	}
	ids := f.store.AllIdentities()
	if len(ids) != 1 || ids[0].Fingerprint != fingerprint {
		t.Fatalf("stored identities: want fingerprint %s, got %v", fingerprint, ids)
	}
}

func TestAdd_lockedVaultReportsLocked(t *testing.T) {
	f := startVaultAgent(t, false)
	path, _ := writeTestKey(t, f.dir, "id_new", "new-key")
	f.lock(t)

	_, err := Add(f.silentEnv(), []string{path}, true)
	if got := CodeOf(err); got != CodeVaultLocked {
		t.Fatalf("code: want %v, got %v (err %v)", CodeVaultLocked, got, err)
	}
	if !errors.Is(err, agent.ErrVaultLocked) {
		t.Fatalf("errors.Is(agent.ErrVaultLocked): want true for %v", err)
	}
	if HintOf(err) == "" {
		t.Fatal("hint: want the unlock remedy, got none")
	}
}

// TestAdd_keyringAgentIsRejected covers an sshush agent in keys mode: it speaks
// sshush-op, so there is no upgrade to suggest.
func TestAdd_keyringAgentIsRejected(t *testing.T) {
	dir := unixSocketTempDirVaultOps(t)
	path, _ := writeTestKey(t, dir, "id_new", "new-key")
	socketPath := startKeyringAgent(t, dir)

	_, err := Add(Env{SocketPath: socketPath}, []string{path}, true)
	if got := CodeOf(err); got != CodeNotVaultAgent {
		t.Fatalf("code: want %v, got %v (err %v)", CodeNotVaultAgent, got, err)
	}
	if !strings.Contains(err.Error(), "requires a running vault agent") {
		t.Fatalf("message: want it to name the missing vault agent, got %q", err.Error())
	}
	if hint := HintOf(err); hint != "" {
		t.Fatalf("hint: want none for an agent that speaks sshush-op, got %q", hint)
	}
}

// TestAdd_foreignAgentGetsTheReloadHint covers the case worth naming: an
// sshushd left running from an older install looks exactly like a foreign
// agent, and a restart fixes it.
func TestAdd_foreignAgentGetsTheReloadHint(t *testing.T) {
	dir := unixSocketTempDirVaultOps(t)
	path, _ := writeTestKey(t, dir, "id_new", "new-key")
	socketPath := startForeignAgent(t, dir)

	_, err := Add(Env{SocketPath: socketPath}, []string{path}, true)
	if got := CodeOf(err); got != CodeNotVaultAgent {
		t.Fatalf("code: want %v, got %v (err %v)", CodeNotVaultAgent, got, err)
	}
	if hint := HintOf(err); !strings.Contains(hint, "sshush reload") {
		t.Fatalf("hint: want it to name 'sshush reload', got %q", hint)
	}
}

func TestAdd_noAgentReportsNoAgent(t *testing.T) {
	dir := unixSocketTempDirVaultOps(t)
	path, _ := writeTestKey(t, dir, "id_new", "new-key")

	_, err := Add(Env{SocketPath: filepath.Join(dir, "nothing-here.sock")}, []string{path}, true)
	if got := CodeOf(err); got != CodeNoAgent {
		t.Fatalf("code: want %v, got %v (err %v)", CodeNoAgent, got, err)
	}
}

func TestAdd_stopsAtFirstBadPathKeepingEarlierKeys(t *testing.T) {
	f := startVaultAgent(t, false)
	good, _ := writeTestKey(t, f.dir, "id_good", "good-key")
	missing := filepath.Join(f.dir, "not-a-key")

	res, err := Add(f.env(), []string{good, missing}, true)
	if err == nil {
		t.Fatal("add: want an error for the missing path, got nil")
	}
	if len(res.Added) != 1 || res.Added[0] != good {
		t.Fatalf("added: want the first key to stand, got %v", res.Added)
	}
	if len(f.store.AllIdentities()) != 1 {
		t.Fatalf("stored identities: want 1, got %d", len(f.store.AllIdentities()))
	}
}

func TestRemove_byFingerprint(t *testing.T) {
	f := startVaultAgent(t, false)
	_, fingerprint := f.addKey(t, "id_one", "one-key", true)

	res, err := Remove(f.env(), []string{fingerprint})
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(res.Removed) != 1 || res.Removed[0] != fingerprint {
		t.Fatalf("removed: want [%s], got %v", fingerprint, res.Removed)
	}
	if len(f.store.AllIdentities()) != 0 {
		t.Fatalf("stored identities: want 0, got %d", len(f.store.AllIdentities()))
	}
}

func TestRemove_byComment(t *testing.T) {
	f := startVaultAgent(t, false)
	f.addKey(t, "id_one", "one-key", true)

	if _, err := Remove(f.env(), []string{"one-key"}); err != nil {
		t.Fatalf("remove by comment: %v", err)
	}
	if len(f.store.AllIdentities()) != 0 {
		t.Fatalf("stored identities: want 0, got %d", len(f.store.AllIdentities()))
	}
}

func TestRemove_byKeyFilePath(t *testing.T) {
	f := startVaultAgent(t, false)
	path, _ := f.addKey(t, "id_one", "one-key", true)

	if _, err := Remove(f.env(), []string{path}); err != nil {
		t.Fatalf("remove by key path: %v", err)
	}
	if len(f.store.AllIdentities()) != 0 {
		t.Fatalf("stored identities: want 0, got %d", len(f.store.AllIdentities()))
	}
}

func TestRemove_ambiguousCommentIsAnError(t *testing.T) {
	f := startVaultAgent(t, false)
	f.addKey(t, "id_one", "shared", true)
	f.addKey(t, "id_two", "shared", true)

	_, err := Remove(f.env(), []string{"shared"})
	if got := CodeOf(err); got != CodeAmbiguousSelector {
		t.Fatalf("code: want %v, got %v (err %v)", CodeAmbiguousSelector, got, err)
	}
	if hint := HintOf(err); !strings.Contains(hint, "fingerprint") {
		t.Fatalf("hint: want it to point at the fingerprint, got %q", hint)
	}
}

func TestRemove_unknownSelectorNamesIt(t *testing.T) {
	f := startVaultAgent(t, false)
	f.addKey(t, "id_one", "one-key", true)

	_, err := Remove(f.env(), []string{"nope"})
	if got := CodeOf(err); got != CodeIdentityNotFound {
		t.Fatalf("code: want %v, got %v (err %v)", CodeIdentityNotFound, got, err)
	}
	if want := "no vault identity matches nope"; err.Error() != want {
		t.Fatalf("message: want %q, got %q", want, err.Error())
	}
}

// TestRemove_uninitializedVaultReportsNotInitialized pins the precondition
// order the seam settles on: the store is checked before the agent, so a
// missing vault is reported as a missing vault rather than as a missing agent.
func TestRemove_uninitializedVaultReportsNotInitialized(t *testing.T) {
	f := startVaultAgent(t, false)
	env := f.env()
	env.VaultPath = filepath.Join(f.dir, "no-such-vault.json")

	_, err := Remove(env, []string{"nope"})
	if got := CodeOf(err); got != CodeVaultNotInitialized {
		t.Fatalf("code: want %v, got %v (err %v)", CodeVaultNotInitialized, got, err)
	}
}

func TestRemove_stopsAtFirstBadSelectorKeepingEarlierRemovals(t *testing.T) {
	f := startVaultAgent(t, false)
	f.addKey(t, "id_one", "one-key", true)
	f.addKey(t, "id_two", "two-key", true)

	res, err := Remove(f.env(), []string{"one-key", "nope", "two-key"})
	if err == nil {
		t.Fatal("remove: want an error for the unknown selector, got nil")
	}
	if len(res.Removed) != 1 {
		t.Fatalf("removed: want the first selector to stand, got %v", res.Removed)
	}
	ids := f.store.AllIdentities()
	if len(ids) != 1 || ids[0].Comment != "two-key" {
		t.Fatalf("stored identities: want two-key left, got %v", ids)
	}
}

// TestRemove_promptsAtMostOnceForManySelectors is the cheap proxy for "one
// session per unit of work": if the preamble ever moved inside the selector
// loop, the user would be asked three times.
func TestRemove_promptsAtMostOnceForManySelectors(t *testing.T) {
	f := startVaultAgent(t, false)
	f.addKey(t, "id_one", "one-key", true)
	f.addKey(t, "id_two", "two-key", true)
	f.addKey(t, "id_three", "three-key", true)
	f.lock(t)

	if _, err := Remove(f.env(), []string{"one-key", "two-key", "three-key"}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if f.ask.calls != 1 {
		t.Fatalf("passphrase prompts: want 1, got %d", f.ask.calls)
	}
}
