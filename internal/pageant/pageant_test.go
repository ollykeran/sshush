package pageant

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"strings"
	"testing"

	sshagent "golang.org/x/crypto/ssh/agent"
)

// keyringWith returns a keyring holding n ed25519 keys, each with comment.
func keyringWith(t *testing.T, n int, comment string) sshagent.Agent {
	t.Helper()
	ring := sshagent.NewKeyring()
	for range n {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if err := ring.Add(sshagent.AddedKey{PrivateKey: priv, Comment: comment}); err != nil {
			t.Fatal(err)
		}
	}
	return ring
}

// requestIdentities is SSH_AGENTC_REQUEST_IDENTITIES, framed.
var requestIdentities = []byte{0, 0, 0, 1, 11}

func TestExchange_listsTheKeyringsKeys(t *testing.T) {
	reply := Exchange(keyringWith(t, 2, "work"), requestIdentities)
	if len(reply) < 9 {
		t.Fatalf("reply too short: %x", reply)
	}
	if got := int(binary.BigEndian.Uint32(reply)); got != len(reply)-4 {
		t.Fatalf("framed length %d, body %d", got, len(reply)-4)
	}
	if reply[4] != 12 { // SSH_AGENT_IDENTITIES_ANSWER
		t.Fatalf("reply type %d, want 12", reply[4])
	}
	if n := binary.BigEndian.Uint32(reply[5:]); n != 2 {
		t.Fatalf("%d keys, want 2", n)
	}
}

func TestExchange_answersBadRequestsWithFailure(t *testing.T) {
	ring := keyringWith(t, 1, "")
	for name, req := range map[string][]byte{
		"empty":           nil,
		"short header":    {0, 0},
		"length too long": {0, 0, 0, 9, 11},
		"too big":         append([]byte{0, 0, 0x20, 0}, make([]byte, 0x2000)...),
	} {
		if got := Exchange(ring, req); !bytes.Equal(got, failure) {
			t.Errorf("%s: got %x, want SSH_AGENT_FAILURE", name, got)
		}
	}
}

// A reply longer than the client's shared memory would be written past its
// end, so it is refused instead.
func TestExchange_refusesAReplyTooLongForTheClient(t *testing.T) {
	ring := keyringWith(t, 40, strings.Repeat("c", 200))
	if got := Exchange(ring, requestIdentities); !bytes.Equal(got, failure) {
		t.Fatalf("got %d bytes, want SSH_AGENT_FAILURE", len(got))
	}
}
