package readypipe

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/ollykeran/sshush/internal/transport"
)

// EnvVar is the environment variable the parent sets to tell the child which
// named pipe to connect to for the readiness handshake.
const EnvVar = "SSHUSH_READY_PIPE"

// Parent is the CLI-side half of the handshake.
type Parent struct {
	name     string
	l        net.Listener
	accepted chan accepted
	// exited receives the child's exit once Started has been told about it. Nil
	// until then, so a Wait without a child simply never hears of one.
	exited chan error
}

// exitGrace is how long Wait goes on looking for a connection from a child it
// has just seen exit. A child that connected, wrote its error and exited can
// have its exit noticed before its connection is handed over by Accept, and
// what it wrote is the better thing to report.
const exitGrace = 500 * time.Millisecond

// accepted is the outcome of waiting for the child to connect.
type accepted struct {
	conn net.Conn
	err  error
}

// New starts listening on a pipe with a name nobody else will guess or reuse.
func New() (*Parent, error) {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return nil, fmt.Errorf("readypipe: pipe name: %w", err)
	}
	name := fmt.Sprintf(`\\.\pipe\sshush-ready-%d-%x`, os.Getpid(), suffix)
	l, err := transport.Listen(name)
	if err != nil {
		return nil, fmt.Errorf("readypipe: create pipe: %w", err)
	}
	// Accept from the start rather than in Wait: until someone is accepting, a
	// client finds the pipe busy, and the child may well connect first.
	p := &Parent{name: name, l: l, accepted: make(chan accepted, 1)}
	go func() {
		conn, err := l.Accept()
		p.accepted <- accepted{conn, err}
	}()
	return p, nil
}

// Attach sets SSHUSH_READY_PIPE in cmd.Env to the pipe the child should connect
// to. Must be called before cmd.Start().
func (p *Parent) Attach(cmd *exec.Cmd) {
	cmd.Env = append(cmd.Env, EnvVar+"="+p.name)
}

// CloseWrite does nothing on Windows: the parent holds no write end. It exists
// so callers are the same on every platform.
func (p *Parent) CloseWrite() {}

// Started tells the parent which process the child is. Call it once, right
// after cmd.Start() succeeds and before Wait.
//
// A named pipe says nothing about a child that never connects to it: unlike the
// inherited pipe on Unix, there is no end that closes when the process dies. So
// the process itself is watched, and Wait returns as soon as it has gone
// instead of sitting out the timeout. Waiting on the process takes nothing from
// a daemon that does start: it was created detached, and stays so. The watch
// lasts until the daemon exits or this process does, whichever is first.
func (p *Parent) Started(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil || p.exited != nil {
		return
	}
	exited := make(chan error, 1)
	p.exited = exited
	go func() { exited <- cmd.Wait() }()
}

// Close stops listening. Safe to call more than once. Intended as a defer'd
// safety net.
func (p *Parent) Close() {
	if p.l == nil {
		return
	}
	// Closing the listener is what releases the Accept started by New.
	_ = p.l.Close()
	p.l = nil
	if a := <-p.accepted; a.conn != nil {
		_ = a.conn.Close()
	}
}

// Wait blocks until the child signals readiness, signals failure, or the
// timeout elapses.
//
// A connection closed with no data written means success. One closed after
// data means the child wrote its real error text; that text becomes the
// returned error. A child that exits without ever connecting is reported as
// such, at once, when Started was told about it. A timeout means the child
// neither succeeded nor failed within the window.
func (p *Parent) Wait(timeout time.Duration) error {
	if p.l == nil {
		return errors.New("readypipe: already closed")
	}
	deadline := time.Now().Add(timeout)
	notReady := fmt.Errorf("started but not ready after %s", timeout)

	var a accepted
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case a = <-p.accepted:
	case exit := <-p.exited:
		// The exit alone does not mean the child said nothing: see exitGrace.
		// When both are there to be had, the connection wins.
		grace := time.NewTimer(exitGrace)
		defer grace.Stop()
		select {
		case a = <-p.accepted:
		case <-grace.C:
			p.Close()
			return exitedEarly(exit)
		case <-timer.C:
			p.Close()
			return exitedEarly(exit)
		}
	case <-timer.C:
		p.Close()
		return notReady
	}
	// Hand the result back so Close, which drains it, still finds one.
	p.accepted <- accepted{}
	if a.err != nil {
		return fmt.Errorf("readypipe: accept: %w", a.err)
	}
	if a.conn == nil {
		return errors.New("readypipe: already waited")
	}
	conn := a.conn
	defer conn.Close()

	if err := conn.SetReadDeadline(deadline); err != nil {
		return fmt.Errorf("readypipe: set deadline: %w", err)
	}
	data, err := io.ReadAll(conn)
	if err != nil {
		if os.IsTimeout(err) {
			return notReady
		}
		return fmt.Errorf("readypipe: read: %w", err)
	}
	if len(data) > 0 {
		return errors.New(string(data))
	}
	return nil
}

// exitedEarly is the error for a child that exited without connecting. exit is
// what waiting on the process returned: nil for a clean exit, which this early
// is no less of a failure.
func exitedEarly(exit error) error {
	status := "exit status 0"
	if exit != nil {
		status = exit.Error()
	}
	return fmt.Errorf("daemon exited before it was ready (%s)", status)
}

// FromEnv looks for SSHUSH_READY_PIPE in the environment and returns a Child
// connected to that pipe, or nil if the variable is unset or nothing is
// listening there — callers can treat "no pipe" and "bad env" identically by
// just proceeding without signaling.
func FromEnv() *Child {
	name := os.Getenv(EnvVar)
	if !transport.IsPipe(name) {
		return nil
	}
	conn, err := transport.Dial(name)
	if err != nil {
		return nil
	}
	return &Child{w: conn}
}
