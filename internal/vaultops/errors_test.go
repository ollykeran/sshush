package vaultops

import (
	"errors"
	"testing"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/openssh"
	"github.com/ollykeran/sshush/internal/vault"
)

// TestDescribe_mapsEverySentinel is the whole point of the seam: one table
// where a reason becomes a sentence, replacing the switch each front end used
// to keep. Every case also checks the cause survives, so a caller that prefers
// errors.Is over Code is not cut off.
func TestDescribe_mapsEverySentinel(t *testing.T) {
	cases := []struct {
		name     string
		in       error
		verb     string
		selector string
		want     Code
		wantMsg  string
		wantHint bool
	}{
		{"locked", agent.ErrVaultLocked, "add", "", CodeVaultLocked, "vault is locked", true},
		{"agent not found", agent.ErrIdentityNotFound, "load", "abc", CodeIdentityNotFound, "no vault identity matches abc", false},
		{"store not found", vault.ErrIdentityNotFound, "remove", "abc", CodeIdentityNotFound, "no vault identity matches abc", false},
		{"not found, no selector", agent.ErrIdentityNotFound, "load", "", CodeIdentityNotFound, "identity not found in vault", false},
		{"ambiguous", vault.ErrAmbiguousComment, "remove", "dup", CodeAmbiguousSelector, "ambiguous comment: multiple vault identities share that comment", true},
		{"encrypted key", openssh.ErrEncryptedPrivateKey, "remove", "k", CodeEncryptedKey, openssh.ErrEncryptedPrivateKey.Error(), false},
		{"no recovery", agent.ErrNoRecovery, "unlock-recovery", "", CodeNoRecovery, "this vault was created without a recovery phrase, so no phrase can unlock it", false},
		{"wrong phrase", agent.ErrWrongPassphrase, "unlock-recovery", "", CodeWrongPassphrase, "unlock failed: wrong recovery phrase", true},
		{"wrong passphrase", agent.ErrWrongPassphrase, "unlock", "", CodeWrongPassphrase, "unlock failed: wrong passphrase", false},
		{"not locked", agent.ErrNotLocked, "unlock", "", CodeNotLocked, "vault is already unlocked", false},
		{"op unsupported", agent.ErrOpUnsupported, "add", "", CodeNotVaultAgent, "vault add requires a running vault agent", true},
		{"op unknown", agent.ErrOpUnknown, "add", "", CodeNotVaultAgent, "vault add requires a running vault agent", false},
		{"bad request", agent.ErrBadRequest, "add", "", CodeAgentFailed, "add failed: " + agent.ErrBadRequest.Error(), false},
		{"internal", agent.ErrAgentInternal, "load", "", CodeAgentFailed, "load failed: " + agent.ErrAgentInternal.Error(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := describe(tc.in, tc.verb, tc.selector)
			if code := CodeOf(got); code != tc.want {
				t.Fatalf("code: want %v, got %v", tc.want, code)
			}
			if got.Error() != tc.wantMsg {
				t.Fatalf("message: want %q, got %q", tc.wantMsg, got.Error())
			}
			if hint := HintOf(got); (hint != "") != tc.wantHint {
				t.Fatalf("hint present: want %v, got %q", tc.wantHint, hint)
			}
			if !errors.Is(got, tc.in) {
				t.Fatalf("errors.Is(%v): want the cause to survive", tc.in)
			}
		})
	}
}

func TestDescribe_nilStaysNil(t *testing.T) {
	if err := describe(nil, "add", ""); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

// TestDescribe_keepsAnExistingOpError stops a verb that already phrased a
// failure from having it rewritten as a generic agent failure.
func TestDescribe_keepsAnExistingOpError(t *testing.T) {
	original := &OpError{Code: CodeVaultNotInitialized, Msg: "vault not found"}
	if got := describe(original, "remove", "x"); got != error(original) {
		t.Fatalf("want the original OpError back, got %v", got)
	}
}

// TestOpError_errorReturnsMsgWithoutHint pins the two-line CLI render: the
// sentence is the error, the remedy is fetched separately.
func TestOpError_errorReturnsMsgWithoutHint(t *testing.T) {
	e := &OpError{Msg: "vault is locked", Hint: "Unlock it."}
	if e.Error() != "vault is locked" {
		t.Fatalf("message: want %q, got %q", "vault is locked", e.Error())
	}
	if HintOf(e) != "Unlock it." {
		t.Fatalf("hint: want %q, got %q", "Unlock it.", HintOf(e))
	}
}

func TestHintOf_plainErrorHasNoHint(t *testing.T) {
	if hint := HintOf(errors.New("boom")); hint != "" {
		t.Fatalf("hint: want empty, got %q", hint)
	}
}

func TestGateError_hintOnlyWhenAgentDoesNotSpeakOps(t *testing.T) {
	speaks := gateError("add", agent.Backend{Mode: "keys", SpeaksOps: true}, nil)
	if HintOf(speaks) != "" {
		t.Fatalf("hint: want none for an agent that speaks sshush-op, got %q", HintOf(speaks))
	}
	silent := gateError("add", agent.Backend{Mode: "keys"}, nil)
	if HintOf(silent) == "" {
		t.Fatal("hint: want the 'sshush reload' remedy for an agent that does not")
	}
}

func TestLoadState_string(t *testing.T) {
	cases := map[LoadState]string{LoadUnknown: "n/a", LoadNo: "no", LoadYes: "yes"}
	for state, want := range cases {
		if got := state.String(); got != want {
			t.Fatalf("LoadState(%d): want %q, got %q", state, want, got)
		}
	}
}
