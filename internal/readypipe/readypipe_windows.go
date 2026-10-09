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
}

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
// returned error. A timeout means the child neither succeeded nor failed
// within the window — including a child that died before it could connect.
func (p *Parent) Wait(timeout time.Duration) error {
	if p.l == nil {
		return errors.New("readypipe: already closed")
	}
	deadline := time.Now().Add(timeout)
	notReady := fmt.Errorf("started but not ready after %s", timeout)

	var conn net.Conn
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case a := <-p.accepted:
		// Hand the result back so Close, which drains it, still finds one.
		p.accepted <- accepted{}
		if a.err != nil {
			return fmt.Errorf("readypipe: accept: %w", a.err)
		}
		if a.conn == nil {
			return errors.New("readypipe: already waited")
		}
		conn = a.conn
	case <-timer.C:
		p.Close()
		return notReady
	}
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
