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
	v := ViewOf(Snapshot{Err: errors.New("dial failed")})
	if v.Icon != IconUnreachable || v.CanUnlock || v.CanLock || len(v.Keys) != 0 {
		t.Errorf("unreachable: got %+v", v)
	}
	if v.Status == "" {
		t.Error("unreachable: want a status line saying why nothing is listed")
	}
}

func TestViewOfLockedVaultOffersUnlockOnly(t *testing.T) {
	v := ViewOf(Snapshot{Backend: agent.Backend{Mode: "vault", VaultLocked: true, SpeaksOps: true}})
	if v.Icon != IconLocked || !v.CanUnlock || v.CanLock {
		t.Errorf("locked vault: got %+v", v)
	}
}

func TestViewOfUnlockedVaultListsKeysAndOffersLock(t *testing.T) {
	keys := []*sshagent.Key{testKey(t, "work"), testKey(t, "")}
	v := ViewOf(Snapshot{Backend: agent.Backend{Mode: "vault", SpeaksOps: true}, Keys: keys})
	if v.Icon != IconUnlocked || v.CanUnlock || !v.CanLock {
		t.Errorf("unlocked vault: got %+v", v)
	}
	if len(v.Keys) != 2 || v.Status != "" {
		t.Fatalf("unlocked vault: want 2 keys and no status, got %+v", v)
	}
	if !strings.HasPrefix(v.Keys[0], "work  SHA256:") {
		t.Errorf("label with comment: got %q", v.Keys[0])
	}
	if !strings.HasPrefix(v.Keys[1], "ssh-ed25519  SHA256:") {
		t.Errorf("label without comment falls back to key type: got %q", v.Keys[1])
	}
	if !strings.Contains(v.Tooltip, "2 keys") {
		t.Errorf("tooltip: got %q", v.Tooltip)
	}
}

func TestViewOfKeyringHasNothingToLock(t *testing.T) {
	v := ViewOf(Snapshot{Backend: agent.Backend{Mode: "keys", SpeaksOps: true}, Keys: []*sshagent.Key{testKey(t, "a")}})
	if v.Icon != IconKeys || v.CanUnlock || v.CanLock || len(v.Keys) != 1 {
		t.Errorf("keyring: got %+v", v)
	}
	if !strings.Contains(v.Tooltip, "1 key") || strings.Contains(v.Tooltip, "1 keys") {
		t.Errorf("tooltip: got %q", v.Tooltip)
	}
}

func TestViewOfEmptyAgentSaysSo(t *testing.T) {
	v := ViewOf(Snapshot{Backend: agent.Backend{Mode: "vault", SpeaksOps: true}})
	if v.Status != "No keys loaded" || len(v.Keys) != 0 {
		t.Errorf("empty agent: got %+v", v)
	}
}

func TestViewEqual(t *testing.T) {
	a := View{Icon: IconKeys, Keys: []string{"x"}}
	b := View{Icon: IconKeys, Keys: []string{"x"}}
	if !a.Equal(b) {
		t.Error("identical views compare unequal")
	}
	b.Keys = []string{"y"}
	if a.Equal(b) {
		t.Error("views with different keys compare equal")
	}
}
