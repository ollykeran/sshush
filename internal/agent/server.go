package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/ollykeran/sshush/internal/style"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// ErrAlreadyRunning reports that another process is already listening on the
// socket. [ListenAndServe] returns it wrapped for display; use errors.Is.
var ErrAlreadyRunning = errors.New("agent: already running on socket")

// errStyled wraps an error with a styled message for display; Unwrap() preserves errors.Is.
type errStyled struct {
	err    error
	styled string
}

func (e *errStyled) Error() string { return e.styled }
func (e *errStyled) Unwrap() error { return e.err }

// Option configures optional ListenAndServe behavior.
type Option func(*options)

type options struct {
	ready func()
}

// WithReady registers a callback invoked once the listener is accepting
// connections, before ListenAndServe blocks in its accept loop.
func WithReady(fn func()) Option {
	return func(o *options) { o.ready = fn }
}

// ListenAndServe serves the SSH agent protocol on a Unix socket at socketPath
// until ctx is cancelled, handing every accepted connection to keyring. Because
// all connections share the one keyring, agent state is per-process rather than
// per-connection.
//
// It returns [ErrAlreadyRunning] when something is already listening on the path,
// and removes a stale socket otherwise. Cancelling ctx closes the listener, so a
// normal shutdown also returns a non-nil error.
func ListenAndServe(ctx context.Context, socketPath string, keyring sshagent.ExtendedAgent, opts ...Option) error {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0700); err != nil {
		return fmt.Errorf("agent: create socket directory %s: %w", filepath.Dir(socketPath), err)
	}
	if conn, err := net.Dial("unix", socketPath); err == nil {
		conn.Close()
		return &errStyled{err: ErrAlreadyRunning, styled: style.Err(ErrAlreadyRunning.Error())}
	}
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("agent: listen on %s: %w", socketPath, err)
	}
	defer listener.Close()

	if o.ready != nil {
		o.ready()
	}

	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			return fmt.Errorf("agent: accept connection: %w", err)
		}
		go sshagent.ServeAgent(keyring, conn)
	}
}
