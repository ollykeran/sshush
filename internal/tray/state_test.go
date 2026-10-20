package tray

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/agent"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func testKey(t *testing.T, comment string) *sshagent.Key {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return &sshagent.Key{Format: sshPub.Type(), Blob: sshPub.Marshal(), Comment: comment}
}

func TestViewOfUnreachable(t *testing.T) {
	v := ViewOf(Snapshot{Err: errors.New("dial failed")}, true)
	if v.Icon != IconUnreachable || v.Heading != "Agent not running" || len(v.Keys) != 0 {
		t.Errorf("unreachable: got %+v", v)
	}
	if v := ViewOf(Snapshot{Err: errors.New("dial failed")}, false); v.Heading != "Agent not reachable" {
		t.Errorf("unreachable unmanaged agent: heading %q", v.Heading)
	}
}

func TestViewOfLockedVault(t *testing.T) {
	v := ViewOf(Snapshot{Backend: agent.Backend{Mode: "vault", VaultLocked: true, SpeaksOps: true}}, true)
	if v.Icon != IconLocked || v.Heading != "Vault agent, locked" || len(v.Keys) != 0 || v.Status != "" {
		t.Errorf("locked vault: got %+v", v)
	}
}

func TestViewOfUnlockedVaultListsKeys(t *testing.T) {
	keys := []*sshagent.Key{testKey(t, "work"), testKey(t, "")}
	v := ViewOf(Snapshot{Backend: agent.Backend{Mode: "vault", SpeaksOps: true}, Keys: keys}, true)
	if v.Icon != IconUnlocked || v.Heading != "Vault agent, unlocked" {
		t.Errorf("unlocked vault: got %+v", v)
	}
	if len(v.Keys) != 2 || v.Status != "" {
		t.Fatalf("unlocked vault: want 2 keys and no status, got %+v", v)
	}
	if !strings.HasPrefix(v.Keys[0].Label, "work  SHA256:") {
		t.Errorf("label with comment: got %q", v.Keys[0].Label)
	}
	if !strings.HasPrefix(v.Keys[1].Label, "ssh-ed25519  SHA256:") {
		t.Errorf("label without comment falls back to key type: got %q", v.Keys[1].Label)
	}
	if !strings.Contains(v.Tooltip, "2 keys") {
		t.Errorf("tooltip: got %q", v.Tooltip)
	}
}

func TestViewOfKeyring(t *testing.T) {
	v := ViewOf(Snapshot{Backend: agent.Backend{Mode: "keys", SpeaksOps: true}, Keys: []*sshagent.Key{testKey(t, "a")}}, true)
	if v.Icon != IconKeys || v.Heading != "Key agent, keys in memory" || len(v.Keys) != 1 {
		t.Errorf("keyring: got %+v", v)
	}
	if !strings.Contains(v.Tooltip, "1 key") || strings.Contains(v.Tooltip, "1 keys") {
		t.Errorf("tooltip: got %q", v.Tooltip)
	}
	if v := ViewOf(Snapshot{Backend: agent.Backend{Mode: "keys"}}, false); v.Heading != "External agent" {
		t.Errorf("agent that speaks no sshush ops: heading %q", v.Heading)
	}
}

func TestViewOfEmptyAgentSaysSo(t *testing.T) {
	v := ViewOf(Snapshot{Backend: agent.Backend{Mode: "vault", SpeaksOps: true}}, true)
	if v.Status != "No keys loaded" || len(v.Keys) != 0 {
		t.Errorf("empty agent: got %+v", v)
	}
}

func TestViewEqual(t *testing.T) {
	a := View{Icon: IconKeys, Heading: "h", Keys: []KeyItem{{Label: "x"}}}
	b := View{Icon: IconKeys, Heading: "h", Keys: []KeyItem{{Label: "x"}}}
	if !a.Equal(b) {
		t.Error("identical views compare unequal")
	}
	b.Keys = []KeyItem{{Label: "y"}}
	if a.Equal(b) {
		t.Error("views with different keys compare equal")
	}
	b = a
	b.Heading = "other"
	if a.Equal(b) {
		t.Error("views with different headings compare equal")
	}
}

func TestViewOfCarriesFingerprints(t *testing.T) {
	k := testKey(t, "fp")
	v := ViewOf(Snapshot{Backend: agent.Backend{Mode: "keys"}, Keys: []*sshagent.Key{k}}, true)
	pub, err := ssh.ParsePublicKey(k.Blob)
	if err != nil {
		t.Fatal(err)
	}
	if v.Keys[0].Fingerprint != ssh.FingerprintSHA256(pub) {
		t.Errorf("fingerprint: got %q", v.Keys[0].Fingerprint)
	}
}
