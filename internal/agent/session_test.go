package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// fakeVaultAgent answers the vault-locked extension the way a vault backend
// does, so Session.Backend can be tested without importing internal/vault
// (which imports this package). Every other operation falls through to the
// embedded keyring.
type fakeVaultAgent struct {
	sshagent.ExtendedAgent
	resp []byte
}

func (f *fakeVaultAgent) Extension(extensionType string, _ []byte) ([]byte, error) {
	if extensionType == extensionVaultLocked {
		return f.resp, nil
	}
	// Must be the bare sentinel: ServeAgent compares it with == , not errors.Is.
	return nil, sshagent.ErrExtensionUnsupported
}

// openSession opens a Session against socketPath and closes it when the test ends.
func openSession(t *testing.T, socketPath string) *Session {
	t.Helper()
	s, err := Open(socketPath)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSessionOpen_socketMissing(t *testing.T) {
	socketPath := filepath.Join(unixSocketTempDir(t), "absent.sock")
	s, err := Open(socketPath)
	if err == nil {
		_ = s.Close()
		t.Fatal("want error opening a socket with no listener, got nil")
	}
	if s != nil {
		t.Errorf("session: want nil on error, got %v", s)
	}
	if !strings.Contains(err.Error(), socketPath) {
		t.Errorf("error: want it to name %q, got %q", socketPath, err.Error())
	}
}

func TestSessionOpen_emptyPath(t *testing.T) {
	s, err := Open("")
	if err == nil {
		_ = s.Close()
		t.Fatal("want error opening an empty socket path, got nil")
	}
	if s != nil {
		t.Errorf("session: want nil on error, got %v", s)
	}
}

func TestSessionList(t *testing.T) {
	socketPath, _ := startServerKeyring(t, "session-list")
	s := openSession(t, socketPath)

	keys, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("want 1 key, got %d", len(keys))
	}
	if keys[0].Comment != "session-list" {
		t.Errorf("comment: want %q, got %q", "session-list", keys[0].Comment)
	}
}

func TestSessionAddKeyFromPath(t *testing.T) {
	socketPath, _ := startServerKeyring(t, "existing")
	s := openSession(t, socketPath)

	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, mustMarshalKey(t, "added-key"), 0600); err != nil {
		t.Fatal(err)
	}
	pubKey, _, _, err := ParseKeyFromPath(path)
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}
	fp := ssh.FingerprintSHA256(pubKey)

	if err := s.AddKeyFromPath(path); err != nil {
		t.Fatalf("add key from path: %v", err)
	}

	keys, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("want 2 keys after add, got %d", len(keys))
	}
	// The fingerprint-to-filepath registration is what cli edit relies on to
	// find a key's source file.
	if got := GetFilepath(fp); got != path {
		t.Errorf("registered filepath: want %q, got %q", path, got)
	}
}

func TestSessionRemoveByFingerprint(t *testing.T) {
	socketPath, _ := startServerKeyring(t, "to-remove")
	s := openSession(t, socketPath)

	keys, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	fp := ssh.FingerprintSHA256(keys[0])

	removed, err := s.RemoveByFingerprint(fp)
	if err != nil {
		t.Fatalf("remove by fingerprint: %v", err)
	}
	if !removed {
		t.Error("removed: want true for a fingerprint the agent holds, got false")
	}
	after, err := s.List()
	if err != nil {
		t.Fatalf("list after remove: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("want 0 keys after remove, got %d", len(after))
	}
}

func TestSessionRemoveByFingerprint_unknownIsNotAnError(t *testing.T) {
	socketPath, _ := startServerKeyring(t, "kept")
	s := openSession(t, socketPath)

	removed, err := s.RemoveByFingerprint("SHA256:definitely-not-a-real-fingerprint")
	if err != nil {
		t.Fatalf("remove by unknown fingerprint: want nil error, got %v", err)
	}
	if removed {
		t.Error("removed: want false for an unknown fingerprint, got true")
	}
}

func TestSessionLockUnlock(t *testing.T) {
	socketPath, _ := startServerKeyring(t, "lock-test")
	s := openSession(t, socketPath)

	passphrase := []byte("correct horse")
	if err := s.Lock(passphrase); err != nil {
		t.Fatalf("lock: %v", err)
	}
	keys, err := s.List()
	if err != nil {
		t.Fatalf("list while locked: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("want 0 keys while locked, got %d", len(keys))
	}
	if err := s.Unlock(passphrase); err != nil {
		t.Fatalf("unlock: %v", err)
	}

	// Pass-through methods must not wrap: cli/unlock.go matches this text exactly.
	// Note the agent's own error text ("agent: not locked") does not survive the
	// wire — ServeAgent sends a bare failure code and the client synthesises
	// "agent: failure" — so this is the only text a caller can ever match on.
	err = s.Unlock(passphrase)
	if err == nil {
		t.Fatal("want error unlocking an unlocked agent, got nil")
	}
	if err.Error() != "agent: failure" {
		t.Errorf("error: want %q unwrapped, got %q", "agent: failure", err.Error())
	}
}

func TestSessionSign(t *testing.T) {
	socketPath, _ := startServerKeyring(t, "sign-test")
	s := openSession(t, socketPath)

	keys, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	data := []byte("sshush-test")
	sig, err := s.Sign(keys[0], data)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := keys[0].Verify(data, sig); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestSessionBackend_keys(t *testing.T) {
	socketPath, _ := startServerKeyring(t, "backend-keys")
	s := openSession(t, socketPath)

	b, err := s.Backend()
	if err != nil {
		t.Fatalf("backend: %v", err)
	}
	if b.Mode != "keys" {
		t.Errorf("mode: want %q, got %q", "keys", b.Mode)
	}
	if b.VaultLocked {
		t.Error("vault locked: want false for a keyring backend, got true")
	}
}

func TestSessionBackend_vault(t *testing.T) {
	for _, tc := range []struct {
		name       string
		resp       []byte
		wantLocked bool
	}{
		{name: "unlocked", resp: []byte{0}, wantLocked: false},
		{name: "locked", resp: []byte{1}, wantLocked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ext := &fakeVaultAgent{ExtendedAgent: keyringWithKey(t, "vault-key"), resp: tc.resp}
			s := openSession(t, startServerAgent(t, ext))

			b, err := s.Backend()
			if err != nil {
				t.Fatalf("backend: %v", err)
			}
			if b.Mode != "vault" {
				t.Errorf("mode: want %q, got %q", "vault", b.Mode)
			}
			if b.VaultLocked != tc.wantLocked {
				t.Errorf("vault locked: want %v, got %v", tc.wantLocked, b.VaultLocked)
			}
		})
	}
}

func TestSessionBackend_garbageResponse(t *testing.T) {
	ext := &fakeVaultAgent{ExtendedAgent: keyringWithKey(t, "vault-key"), resp: []byte{7, 7}}
	s := openSession(t, startServerAgent(t, ext))

	b, err := s.Backend()
	if err != nil {
		t.Fatalf("backend: %v", err)
	}
	// The extension answered, so it is a vault; only a single 0x01 byte means locked.
	if b.Mode != "vault" {
		t.Errorf("mode: want %q, got %q", "vault", b.Mode)
	}
	if b.VaultLocked {
		t.Error("vault locked: want false for a malformed response, got true")
	}
}

func TestSessionClose_idempotent(t *testing.T) {
	socketPath, _ := startServerKeyring(t, "close-test")
	s, err := Open(socketPath)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second close: want nil, got %v", err)
	}
}

func TestSessionUseAfterClose(t *testing.T) {
	socketPath, _ := startServerKeyring(t, "use-after-close")
	s, err := Open(socketPath)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Must report an error rather than panicking on the closed connection.
	if _, err := s.List(); err == nil {
		t.Error("list after close: want error, got nil")
	}
}

func TestSessionSocketPath(t *testing.T) {
	socketPath, _ := startServerKeyring(t, "socket-path")
	s := openSession(t, socketPath)

	if got := s.SocketPath(); got != socketPath {
		t.Errorf("socket path: want %q, got %q", socketPath, got)
	}
}

func TestSessionExtension_unsupportedPassesThrough(t *testing.T) {
	socketPath, _ := startServerKeyring(t, "extension-test")
	s := openSession(t, socketPath)

	// cli/vault.go distinguishes an unsupported extension from a failed one with
	// errors.Is, so the sentinel must survive the Session.
	_, err := s.Extension("no-such-extension", nil)
	if !errors.Is(err, sshagent.ErrExtensionUnsupported) {
		t.Errorf("error: want ErrExtensionUnsupported, got %v", err)
	}
}
