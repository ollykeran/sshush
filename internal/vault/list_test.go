package vault

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ollykeran/sshush/internal/agent"
	ssh "golang.org/x/crypto/ssh"
)

func TestListing_roundTrip(t *testing.T) {
	want := Listing{
		VaultPath: `C:\Users\me\.config\sshush\vault.json`,
		Identities: []IdentityInfo{
			{Fingerprint: "SHA256:one", Comment: "work laptop", KeyType: "?", Autoload: true, Filepath: "/home/me/.ssh/id_ed25519", PublicKey: []byte("not a real key")},
			{Fingerprint: "SHA256:two", KeyType: "?", PublicKey: []byte{}},
		},
	}
	got, err := decodeListing(encodeListing(want))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.VaultPath != want.VaultPath {
		t.Errorf("vault path: want %q, got %q", want.VaultPath, got.VaultPath)
	}
	if len(got.Identities) != len(want.Identities) {
		t.Fatalf("identities: want %d, got %d", len(want.Identities), len(got.Identities))
	}
	for i, w := range want.Identities {
		g := got.Identities[i]
		if g.Fingerprint != w.Fingerprint || g.Comment != w.Comment || g.KeyType != w.KeyType ||
			g.Autoload != w.Autoload || g.Filepath != w.Filepath || !bytes.Equal(g.PublicKey, w.PublicKey) {
			t.Errorf("identity %d: want %+v, got %+v", i, w, g)
		}
	}
}

func TestListing_emptyVault(t *testing.T) {
	got, err := decodeListing(encodeListing(Listing{VaultPath: "/v.json"}))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.VaultPath != "/v.json" || len(got.Identities) != 0 {
		t.Fatalf("want an empty listing for /v.json, got %+v", got)
	}
}

// TestDecodeListing_ignoresWhatANewerDaemonAdds is the reason each identity is
// its own length-prefixed record: a daemon that learns to say more must not
// break the clients that were already installed.
func TestDecodeListing_ignoresWhatANewerDaemonAdds(t *testing.T) {
	record := ssh.Marshal(struct {
		Fingerprint string
		PublicKey   []byte
		Comment     string
		Filepath    string
		Autoload    bool
		LastUsed    string // not a field this version knows
	}{Fingerprint: "SHA256:one", Comment: "c", Autoload: true, LastUsed: "yesterday"})

	data := ssh.Marshal(struct {
		VaultPath string
		Count     uint32
	}{VaultPath: "/v.json", Count: 1})
	data = append(data, ssh.Marshal(struct{ Record []byte }{record})...)
	data = append(data, ssh.Marshal(struct{ Trailer string }{"something appended to the listing"})...)

	got, err := decodeListing(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Identities) != 1 || got.Identities[0].Fingerprint != "SHA256:one" || !got.Identities[0].Autoload {
		t.Fatalf("want the one identity read past the unknown fields, got %+v", got.Identities)
	}
}

func TestDecodeListing_truncatedIsAnError(t *testing.T) {
	data := encodeListing(Listing{
		VaultPath:  "/v.json",
		Identities: []IdentityInfo{{Fingerprint: "SHA256:one"}, {Fingerprint: "SHA256:two"}},
	})
	for _, n := range []int{0, 3, len(data) - 1} {
		if _, err := decodeListing(data[:n]); err == nil {
			t.Errorf("decode of %d of %d bytes: want an error, got nil", n, len(data))
		}
	}
}

// TestIdentities_listsWhatListDoesNot is the point of the operation: the plain
// agent List only shows what is loaded this session, and a client that cannot
// read the vault file has no other way to see the rest.
func TestIdentities_listsWhatListDoesNot(t *testing.T) {
	dir := unixSocketTempDirSessionOps(t)
	keyPath := writeTestKey(t, dir, "stored-not-loaded")
	socketPath, store := startVaultAgentSocket(t, dir, []byte("listing-test"))
	s := openTestSession(t, socketPath)
	if err := AddPrivateKeyFile(s, keyPath, true); err != nil {
		t.Fatalf("add key: %v", err)
	}
	stored := store.AllIdentities()[0]
	if err := SessionUnload(s, stored.Fingerprint); err != nil {
		t.Fatalf("session unload: %v", err)
	}
	if listed, err := s.List(); err != nil || len(listed) != 0 {
		t.Fatalf("agent list after unload: want 0 keys, got %d (err %v)", len(listed), err)
	}

	got, err := Identities(s)
	if err != nil {
		t.Fatalf("identities: %v", err)
	}
	if got.VaultPath != filepath.Join(dir, "vault.json") {
		t.Errorf("vault path: want the agent's vault file, got %q", got.VaultPath)
	}
	if len(got.Identities) != 1 {
		t.Fatalf("identities: want 1, got %d", len(got.Identities))
	}
	id := got.Identities[0]
	if id.Fingerprint != stored.Fingerprint || id.Comment != "stored-not-loaded" || id.KeyType != "ssh-ed25519" ||
		!id.Autoload || id.Filepath != keyPath || !bytes.Equal(id.PublicKey, stored.PublicKey) {
		t.Fatalf("identity: want what the store holds, got %+v", id)
	}
}

// TestIdentities_lockedVaultRefuses: a locked agent lists no keys, and this
// operation must not be the way around that.
func TestIdentities_lockedVaultRefuses(t *testing.T) {
	dir := unixSocketTempDirSessionOps(t)
	s := openTestSession(t, startLockedVaultAgentSocket(t, dir, []byte("listing-locked")))

	_, err := Identities(s)
	if !errors.Is(err, agent.ErrVaultLocked) {
		t.Fatalf("want agent.ErrVaultLocked, got %v", err)
	}
}
