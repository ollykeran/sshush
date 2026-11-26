// Package pageant lets the agent answer programs that look for PuTTY's
// Pageant rather than an OpenSSH agent: MobaXterm ("Use external Pageant"),
// WinSCP, FileZilla, PuTTY itself. They find it by a hidden window of class
// "Pageant" and hand it requests through WM_COPYDATA and shared memory, so
// there is no socket or pipe to point them at.
//
// The window and its message loop come from github.com/ndbeals/winssh-pageant.
// Each request it receives is the same agent protocol message ssh sends on the
// pipe, so it is answered by the same keyring through Exchange.
package pageant

import (
	"encoding/binary"
	"io"
	"net"
	"time"

	sshagent "golang.org/x/crypto/ssh/agent"
)

// Status says what Serve did.
type Status int

const (
	// Off: not on Windows, so there is no Pageant to be.
	Off Status = iota
	// Serving: this process now answers as Pageant.
	Serving
	// Taken: another program already answers as Pageant (PuTTY's own Pageant,
	// KeeAgent, gpg-agent with PuTTY support, another sshush), and only one
	// window can.
	Taken
	// Failed: the window could not be made.
	Failed
)

// MaxMessage is the largest request or reply Exchange passes: the size of the
// shared memory PuTTY-derived clients map for a request (PuTTY's
// AGENT_MAX_MSGLEN). A reply is written into that same memory, so a longer one
// would run past its end.
const MaxMessage = 8192

// replyTimeout bounds one request. A vault agent may wait on the user, but a
// Pageant client blocks in SendMessage until it is answered, so not forever.
const replyTimeout = 60 * time.Second

// failure is SSH_AGENT_FAILURE, framed.
var failure = []byte{0, 0, 0, 1, 5}

// Exchange answers one framed agent request (its 4-byte big-endian length
// then the message) with ext and returns the framed reply. Anything that goes
// wrong, a reply too long for the client's memory among them, is answered with
// SSH_AGENT_FAILURE: a Pageant client gets a reply either way.
func Exchange(ext sshagent.Agent, req []byte) []byte {
	if len(req) < 4 || len(req) > MaxMessage || int(binary.BigEndian.Uint32(req))+4 != len(req) {
		return failure
	}
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = sshagent.ServeAgent(ext, server)
		server.Close()
	}()
	defer func() { client.Close(); <-done }()
	_ = client.SetDeadline(time.Now().Add(replyTimeout))
	if _, err := client.Write(req); err != nil {
		return failure
	}
	var head [4]byte
	if _, err := io.ReadFull(client, head[:]); err != nil {
		return failure
	}
	n := binary.BigEndian.Uint32(head[:])
	if n == 0 || n > MaxMessage-4 {
		return failure
	}
	reply := make([]byte, 4+n)
	copy(reply, head[:])
	if _, err := io.ReadFull(client, reply[4:]); err != nil {
		return failure
	}
	return reply
}
