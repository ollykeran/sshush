package bridge

import (
	"io"
)

// Relay copies between an agent connection and a pair of streams until either
// side is done, and is all "sshush relay" does once it has dialled the agent.
//
// It returns as soon as one direction ends rather than draining the other. The
// agent protocol is strictly request then response on a connection the client
// holds open, so input ending means the client has gone and there is nobody
// left to answer; waiting for the agent to say something more would leave the
// process running for as long as the agent does.
func Relay(agent io.ReadWriter, in io.Reader, out io.Writer) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(agent, in)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(out, agent)
		done <- struct{}{}
	}()
	<-done
}
