// Package readypipe implements a readiness handshake between a CLI process
// and a daemon it forks: instead of polling for a socket/port to appear, the
// child signals readiness (or failure, with the real error text) over an
// inherited pipe, and the parent blocks on that signal with a timeout as a
// safety net for a truly hung child.
package readypipe

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// EnvVar is the environment variable the parent sets to tell the child which
// inherited file descriptor to use for the readiness pipe.
const EnvVar = "SSHUSH_READY_FD"

// Parent is the CLI-side half of the handshake.
type Parent struct {
	r *os.File
	w *os.File
}

// New creates the underlying pipe.
func New() (*Parent, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("readypipe: create pipe: %w", err)
	}
	return &Parent{r: r, w: w}, nil
}

// Attach wires the pipe's write end into cmd as an inherited file descriptor
// and sets SSHUSH_READY_FD in cmd.Env to the fd number the child will see.
// Must be called before cmd.Start().
func (p *Parent) Attach(cmd *exec.Cmd) {
	fd := 3 + len(cmd.ExtraFiles)
	cmd.ExtraFiles = append(cmd.ExtraFiles, p.w)
	cmd.Env = append(cmd.Env, EnvVar+"="+strconv.Itoa(fd))
}

// CloseWrite closes the parent's own copy of the write end. Call this right
// after cmd.Start() returns so that a child which dies without signaling
// produces EOF instead of leaving Wait blocked until the timeout. Safe to
// call more than once.
func (p *Parent) CloseWrite() {
	if p.w == nil {
		return
	}
	_ = p.w.Close()
	p.w = nil
}

// Close closes both ends of the pipe. Safe to call more than once and after
// CloseWrite. Intended as a defer'd safety net.
func (p *Parent) Close() {
	p.CloseWrite()
	if p.r == nil {
		return
	}
	_ = p.r.Close()
	p.r = nil
}

// Wait blocks until the child signals readiness, signals failure, or the
// timeout elapses.
//
// EOF with no data written means the child closed the pipe to signal
// success. EOF with data written means the child wrote its real error text
// before closing; that text becomes the returned error. A timeout means the
// child neither succeeded nor failed within the window.
func (p *Parent) Wait(timeout time.Duration) error {
	if p.r == nil {
		return errors.New("readypipe: already closed")
	}
	if err := p.r.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("readypipe: set deadline: %w", err)
	}
	data, err := io.ReadAll(p.r)
	if err != nil {
		if os.IsTimeout(err) {
			return fmt.Errorf("started but not ready after %s", timeout)
		}
		return fmt.Errorf("readypipe: read: %w", err)
	}
	if len(data) > 0 {
		return errors.New(string(data))
	}
	return nil
}

// Child is the daemon-side half of the handshake. A nil *Child is valid and
// all methods are no-ops on it, so code that runs without a parent-supplied
// pipe (e.g. sshushd launched by hand, outside `sshush start`/`sshush
// server`) works unchanged.
type Child struct {
	w *os.File
}

// FromEnv looks for SSHUSH_READY_FD in the environment and returns a Child
// wrapping that file descriptor, or nil if the variable is unset or
// malformed (non-numeric or negative) — callers can treat "no pipe" and "bad
// env" identically by just proceeding without signaling.
func FromEnv() *Child {
	v := os.Getenv(EnvVar)
	if v == "" {
		return nil
	}
	fd, err := strconv.Atoi(v)
	if err != nil || fd < 0 {
		return nil
	}
	return &Child{w: os.NewFile(uintptr(fd), "readypipe")}
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
