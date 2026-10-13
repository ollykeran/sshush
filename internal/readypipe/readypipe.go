// Package readypipe implements a readiness handshake between a CLI process
// and a daemon it forks: instead of polling for a socket/port to appear, the
// child signals readiness (or failure, with the real error text) over a pipe
// the parent gave it, and the parent blocks on that signal with a timeout as a
// safety net for a truly hung child.
//
// On Unix the pipe is an anonymous one the child inherits as a file descriptor.
// Windows can neither pass extra descriptors to a child nor put a deadline on an
// anonymous pipe, so there the parent serves a one-off named pipe and the child
// connects to it by name.
package readypipe

import "io"

// Child is the daemon-side half of the handshake. A nil *Child is valid and
// all methods are no-ops on it, so code that runs without a parent-supplied
// pipe (e.g. sshushd launched by hand, outside `sshush start`/`sshush
// server`) works unchanged.
type Child struct {
	w io.WriteCloser
}

// Ready signals successful startup: a bare close, no data written.
func (c *Child) Ready() {
	if c == nil || c.w == nil {
		return
	}
	_ = c.w.Close()
	c.w = nil
}

// Fail signals startup failure: writes err.Error() then closes.
func (c *Child) Fail(err error) {
	if c == nil || c.w == nil {
		return
	}
	_, _ = io.WriteString(c.w, err.Error())
	_ = c.w.Close()
	c.w = nil
}
