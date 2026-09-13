package vaultops

import (
	"path/filepath"
	"testing"
)

func TestSessionLoad_byCommentWithStore(t *testing.T) {
	f := startVaultAgent(t, false)
	_, fingerprint := f.addKey(t, "id_noload", "noload-key", false)

	res, err := SessionLoad(f.env(), []string{"noload-key"})
	if err != nil {
		t.Fatalf("session load by comment: %v", err)
	}
	if len(res.Loaded) != 1 || res.Loaded[0] != fingerprint {
		t.Fatalf("loaded: want [%s], got %v", fingerprint, res.Loaded)
	}
}

// TestSessionLoad_byFingerprintWithoutVaultPath is the TUI's mode: it selects a
// table row, so it already holds the fingerprint and configures no vault path.
func TestSessionLoad_byFingerprintWithoutVaultPath(t *testing.T) {
	f := startVaultAgent(t, false)
	_, fingerprint := f.addKey(t, "id_noload", "noload-key", false)

	if _, err := SessionLoad(Env{SocketPath: f.socketPath}, []string{fingerprint}); err != nil {
		t.Fatalf("session load by fingerprint: %v", err)
	}
}

func TestSessionLoad_nonFingerprintSelectorWithoutVaultPathIsAnError(t *testing.T) {
	f := startVaultAgent(t, false)
	f.addKey(t, "id_noload", "noload-key", false)

	_, err := SessionLoad(Env{SocketPath: f.socketPath}, []string{"noload-key"})
	if got := CodeOf(err); got != CodeNoVaultPath {
		t.Fatalf("code: want %v, got %v (err %v)", CodeNoVaultPath, got, err)
	}
}

// TestSessionLoad_configuredButUninitializedVaultIsAnError: naming a vault that
// is not there is a mistake worth reporting, even for a selector the agent
// could have resolved on its own.
func TestSessionLoad_configuredButUninitializedVaultIsAnError(t *testing.T) {
	f := startVaultAgent(t, false)
	_, fingerprint := f.addKey(t, "id_noload", "noload-key", false)

	env := f.silentEnv()
	env.VaultPath = filepath.Join(f.dir, "no-such-vault.json")
	_, err := SessionLoad(env, []string{fingerprint})
	if got := CodeOf(err); got != CodeVaultNotInitialized {
		t.Fatalf("code: want %v, got %v (err %v)", CodeVaultNotInitialized, got, err)
	}
}

func TestSessionLoad_unknownIdentityFromAgent(t *testing.T) {
	f := startVaultAgent(t, false)

	_, err := SessionLoad(Env{SocketPath: f.socketPath}, []string{"SHA256:notarealfingerprintatall"})
	if got := CodeOf(err); got != CodeIdentityNotFound {
		t.Fatalf("code: want %v, got %v (err %v)", CodeIdentityNotFound, got, err)
	}
}

func TestSetAutoload_togglesPersistedFlag(t *testing.T) {
	f := startVaultAgent(t, false)
	_, fingerprint := f.addKey(t, "id_one", "one-key", true)

	res, err := SetAutoload(f.env(), []string{"one-key"}, false)
	if err != nil {
		t.Fatalf("set autoload off: %v", err)
	}
	if res.On || len(res.Changed) != 1 || res.Changed[0] != fingerprint {
		t.Fatalf("result: want off for [%s], got %+v", fingerprint, res)
	}
	if got := autoloadOf(t, f, fingerprint); got {
		t.Fatal("persisted autoload: want false, got true")
	}

	if _, err := SetAutoload(f.env(), []string{fingerprint}, true); err != nil {
		t.Fatalf("set autoload on: %v", err)
	}
	if got := autoloadOf(t, f, fingerprint); !got {
		t.Fatal("persisted autoload: want true, got false")
	}
}

func TestSetAutoload_lockedVaultReportsLocked(t *testing.T) {
	f := startVaultAgent(t, false)
	_, fingerprint := f.addKey(t, "id_one", "one-key", true)
	f.lock(t)

	_, err := SetAutoload(f.silentEnv(), []string{fingerprint}, false)
	if got := CodeOf(err); got != CodeVaultLocked {
		t.Fatalf("code: want %v, got %v (err %v)", CodeVaultLocked, got, err)
	}
}

// autoloadOf reads the flag back off disk, so the assertion is about what
// survives a daemon restart rather than about in-memory state.
func autoloadOf(t *testing.T, f *fixture, fingerprint string) bool {
	t.Helper()
	for _, id := range f.store.AllIdentities() {
		if id.Fingerprint == fingerprint {
			return id.Autoload
		}
	}
	t.Fatalf("identity %s is not in the vault", fingerprint)
	return false
}
