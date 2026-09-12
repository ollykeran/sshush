package server

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	gliderlabs "github.com/gliderlabs/ssh"
	"golang.org/x/crypto/ssh"
)

// AuthKeySource provides authorized-key checks for the SSH server.
// Implementations must use constant-time comparison when comparing keys.
type AuthKeySource interface {
	Authorized(key ssh.PublicKey) bool
}

// Server is a TCP SSH server that authenticates by public key (and, if Passwords is set,
// by password) and serves each session a shell or the command it asked for.
// It does not depend on config or CLI; all data is passed via struct fields.
type Server struct {
	ListenAddr string
	AuthKeys   AuthKeySource
	// HostKeyPath is the server's host key file. It is created if missing.
	// Empty means an ephemeral key for this process, which every client will see
	// as a host key change, so callers that outlive one run should always set it.
	HostKeyPath string
	// Shell is the shell sessions run, as a path or a name looked up on PATH. Empty
	// means the daemon's own $SHELL, then /bin/bash, then /bin/sh.
	Shell string
	// Passwords, if set, turns on password authentication alongside public keys,
	// every attempt going through a passwordGuard before it reaches Passwords. Nil
	// offers public keys alone.
	Passwords PasswordSource
	// Log, if set, receives a record of what the server does: its startup, every
	// connection and sign-in attempt, each session, and everything it refuses. Nil
	// logs nothing.
	Log *slog.Logger
	// Ready, if set, is called once the TCP listener is accepting
	// connections, before ListenAndServe blocks serving them.
	Ready func()

	// shellPath is Shell resolved to a path, set before any session starts.
	shellPath string

	mu      sync.Mutex
	serving *gliderlabs.Server // set once listening
	closed  bool

	// pending finds a connection's log state by remote address, for the one
	// callback that is given no context.
	pending        sync.Map
	activeSessions atomic.Int64
}

// ListenAndServe starts the SSH server on s.ListenAddr. It does not return until the server exits.
// If HostKeyPath is set, that file is used, and a host key is generated there when the file
// does not exist yet; otherwise an ephemeral in-memory key is used for this process.
// A Shell that cannot be found is an error before anything listens. After Close it
// returns nil.
func (s *Server) ListenAndServe() error {
	if s.Shell != "" {
		path, err := ResolveShell(s.Shell)
		if err != nil {
			return fmt.Errorf("server: shell: %w", err)
		}
		s.shellPath = path
	}
	opts := []gliderlabs.Option{
		gliderlabs.PublicKeyAuth(s.publicKeyAuth),
		s.serveOnlySessions,
		s.observe,
	}
	if s.Passwords != nil {
		guard := newPasswordGuard(s.Passwords)
		opts = append(opts, gliderlabs.PasswordAuth(s.passwordAuth(guard)))
	}
	var hostKey []any
	if s.HostKeyPath != "" {
		if _, err := EnsureHostKey(s.HostKeyPath); err != nil {
			return err
		}
		opts = append(opts, gliderlabs.HostKeyFile(s.HostKeyPath))
		hostKey = []any{"host_key_path", s.HostKeyPath}
		if fingerprint, err := HostKeyFingerprint(s.HostKeyPath); err == nil {
			hostKey = append(hostKey, "host_key", fingerprint)
		}
	} else {
		pem, err := generateHostKeyPEM()
		if err != nil {
			return fmt.Errorf("server: generate host key: %w", err)
		}
		opts = append(opts, gliderlabs.HostKeyPEM(pem))
		hostKey = []any{"host_key", "ephemeral"}
	}

	serving := &gliderlabs.Server{Handler: s.handleSession}
	for _, opt := range opts {
		if err := serving.SetOption(opt); err != nil {
			return fmt.Errorf("server: %w", err)
		}
	}
	ln, err := net.Listen("tcp", s.ListenAddr)
	if err != nil {
		return fmt.Errorf("server: listen %s: %w", s.ListenAddr, err)
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = ln.Close()
		return nil
	}
	s.serving = serving
	s.mu.Unlock()

	listening := append([]any{"addr", ln.Addr().String()}, hostKey...)
	listening = append(listening, "auth_methods", s.offeredMethods(), "shell", loginShell(s.shellPath))
	s.logger().Info("server listening", listening...)
	if s.Ready != nil {
		s.Ready()
	}

	err = serving.Serve(ln)
	if errors.Is(err, gliderlabs.ErrServerClosed) {
		s.waitForSessions(shellKillGrace + time.Second)
		return nil
	}
	return fmt.Errorf("server: listen %s: %w", s.ListenAddr, err)
}

// Close stops the server: the listener first, then every signed-in connection,
// whose sessions end the way they would on a disconnect. ListenAndServe then
// returns nil, once those sessions have had time to end. Closing a server that has
// not started listening yet keeps it from starting.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	serving := s.serving
	s.mu.Unlock()
	if serving == nil {
		return nil
	}
	return serving.Close()
}

// waitForSessions gives the sessions Close hung up on up to limit to finish.
// ListenAndServe returning is what lets the daemon exit, and a session still
// tearing down then would leave its shell without the SIGKILL that follows an
// ignored SIGHUP.
func (s *Server) waitForSessions(limit time.Duration) {
	deadline := time.Now().Add(limit)
	for s.activeSessions.Load() > 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
}

// publicKeyAuth checks key against AuthKeys, noting the key for the auth log line
// x/crypto reports next.
func (s *Server) publicKeyAuth(ctx gliderlabs.Context, key gliderlabs.PublicKey) bool {
	if state := connStateFrom(ctx); state != nil {
		state.noteOfferedKey(key)
	}
	return s.AuthKeys.Authorized(key)
}

// passwordAuth checks a password through guard, noting for the auth log line why
// the guard refused it when that was not the password being wrong, and whether
// this failure locked the address out. The lockout is logged with that line, not
// here: x/crypto logs the attempt only once this handler has returned, and a
// lockout "after 5 failed passwords" read before the fifth would not add up.
func (s *Server) passwordAuth(guard *passwordGuard) gliderlabs.PasswordHandler {
	return func(ctx gliderlabs.Context, password string) bool {
		ok, refusal, lockedOutNow := guard.verify(ctx, ctx.RemoteAddr(), []byte(password))
		if state := connStateFrom(ctx); state != nil {
			lockedOut := ""
			if lockedOutNow {
				lockedOut = addressHost(ctx.RemoteAddr())
			}
			state.notePasswordCheck(refusal, lockedOut)
		}
		return ok
	}
}
