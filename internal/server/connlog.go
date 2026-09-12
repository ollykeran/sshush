package server

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	gliderlabs "github.com/gliderlabs/ssh"
	"golang.org/x/crypto/ssh"
)

type connStateKey struct{}

// connState is what the log knows about one client connection, gathered from the
// separate callbacks gliderlabs and x/crypto make for it.
type connState struct {
	remote    string // the client's address, host:port
	local     string // the address it connected to
	opened    time.Time
	closeOnce sync.Once

	mu            sync.Mutex
	user          string        // the user name the client last tried to sign in as
	authenticated bool          // whether it succeeded
	offeredKey    ssh.PublicKey // the key the next publickey attempt is about
	refusal       string        // why the password guard refused, when not for being wrong
	lockedOut     string        // the host the latest failed password locked out, if it did
	failure       error         // why the handshake failed, if it did
}

func connStateFrom(ctx context.Context) *connState {
	state, _ := ctx.Value(connStateKey{}).(*connState)
	return state
}

func (c *connState) noteOfferedKey(key ssh.PublicKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offeredKey = key
}

func (c *connState) notePasswordCheck(refusal, lockedOut string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refusal, c.lockedOut = refusal, lockedOut
}

// observe installs the callbacks the connection and sign-in log is written from.
func (s *Server) observe(srv *gliderlabs.Server) error {
	srv.ConnCallback = s.onConnect
	srv.ConnectionFailedCallback = s.onHandshakeFailed
	srv.ServerConfigCallback = s.serverConfig
	srv.SessionRequestCallback = s.onSessionRequest
	return nil
}

// onConnect logs a new connection and wraps it so its end is logged too.
func (s *Server) onConnect(ctx gliderlabs.Context, conn net.Conn) net.Conn {
	state := &connState{
		remote: conn.RemoteAddr().String(),
		local:  conn.LocalAddr().String(),
		opened: time.Now(),
	}
	ctx.SetValue(connStateKey{}, state)
	s.pending.Store(state.remote, state)
	s.logger().Info("connection opened", "remote", state.remote, "local", state.local)
	return &loggedConn{Conn: conn, server: s, state: state}
}

// onHandshakeFailed notes why a connection's handshake failed, for the record
// logged when gliderlabs closes it straight after. The callback is given no
// context, so the connection's state is found by its remote address.
func (s *Server) onHandshakeFailed(conn net.Conn, err error) {
	v, ok := s.pending.Load(conn.RemoteAddr().String())
	if !ok {
		return
	}
	state := v.(*connState)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.failure = err
}

// serverConfig gives each connection an x/crypto config whose auth log callback
// knows which connection it is logging for. gliderlabs adds its auth handlers to
// the config afterwards.
func (s *Server) serverConfig(ctx gliderlabs.Context) *ssh.ServerConfig {
	state := connStateFrom(ctx)
	return &ssh.ServerConfig{
		AuthLogCallback: func(conn ssh.ConnMetadata, method string, err error) {
			if state != nil {
				s.logAuth(state, conn.User(), method, err)
			}
		},
	}
}

// logAuth logs one authentication attempt: its method, the user name the client
// gave, where from, whether it was accepted, and for publickey which key. A "none"
// attempt is a client asking what it may use, so it logs what it was offered.
func (s *Server) logAuth(state *connState, user, method string, err error) {
	state.mu.Lock()
	state.user = user
	key, refusal, lockedOut := state.offeredKey, state.refusal, state.lockedOut
	switch method {
	case "publickey":
		state.offeredKey = nil
	case "password":
		state.refusal, state.lockedOut = "", ""
	}
	if err == nil {
		state.authenticated = true
	}
	state.mu.Unlock()

	if method == "none" && err != nil {
		s.logger().Info("auth methods offered", "user", user, "remote", state.remote, "methods", s.offeredMethods())
		return
	}
	attrs := []any{"method", method, "user", user, "remote", state.remote}
	if method == "publickey" && key != nil {
		attrs = append(attrs, "key_type", key.Type(), "fingerprint", ssh.FingerprintSHA256(key))
	}
	if err == nil {
		s.logger().Info("auth accepted", attrs...)
		return
	}
	if refusal == "" {
		refusal = authFailureReason(err)
	}
	if refusal != "" {
		attrs = append(attrs, "reason", refusal)
	}
	s.logger().Info("auth failed", append(attrs, "methods_offered", s.offeredMethods())...)
	// After the failure that caused it, so a lockout "after 5 failures" is read
	// after the fifth: x/crypto logs an attempt only once its handler has returned.
	if method == "password" && lockedOut != "" {
		s.logger().Warn("password lockout", "host", lockedOut,
			"failures", passwordLockoutFailures, "duration", passwordLockout)
	}
}

// authFailureReason is what is worth saying about a failed attempt beyond "it
// failed": nothing when the credential was simply refused, and x/crypto's own
// explanation otherwise, such as a method that is not offered.
func authFailureReason(err error) string {
	switch {
	case err.Error() == "permission denied", errors.Is(err, ssh.ErrNoAuth):
		return ""
	case strings.HasSuffix(err.Error(), "auth not configured"):
		return "method not offered"
	default:
		return err.Error()
	}
}

// offeredMethods lists the authentication methods clients are offered, in the
// comma-separated form the protocol uses.
func (s *Server) offeredMethods() string {
	if s.Passwords != nil {
		return "publickey,password"
	}
	return "publickey"
}

// onSessionRequest logs and refuses subsystem requests, which is how sftp and so
// plain scp ask to start. Shells and commands are let through, to be logged by
// the session itself. Refusing here answers the request with the same failure an
// unhandled subsystem gets, so clients see no difference.
func (s *Server) onSessionRequest(sess gliderlabs.Session, requestType string) bool {
	if requestType != "subsystem" {
		return true
	}
	s.logger().Info("subsystem refused", "subsystem", sess.Subsystem(),
		"user", sess.User(), "remote", remoteOf(sess.Context()))
	return false
}

// loggedConn logs its connection's end when gliderlabs closes it, which happens
// once for every connection: after a failed handshake, a client that left, or the
// server's own Close.
type loggedConn struct {
	net.Conn
	server *Server
	state  *connState
}

func (c *loggedConn) Close() error {
	err := c.Conn.Close()
	c.state.closeOnce.Do(func() {
		c.server.pending.Delete(c.state.remote)
		c.server.logDisconnect(c.state)
	})
	return err
}

// logDisconnect logs how a connection ended: who it was, whether it had signed in,
// how long it lasted, and — for one that never signed in — why, when that was
// something other than the client going away.
func (s *Server) logDisconnect(state *connState) {
	state.mu.Lock()
	user, authenticated, failure := state.user, state.authenticated, state.failure
	state.mu.Unlock()

	attrs := []any{"remote", state.remote}
	if user != "" {
		attrs = append(attrs, "user", user)
	}
	attrs = append(attrs, "authenticated", authenticated, "duration", time.Since(state.opened).Round(time.Millisecond))
	if !authenticated {
		if reason := handshakeFailureReason(failure); reason != "" {
			attrs = append(attrs, "reason", reason)
		}
	}
	s.logger().Info("connection closed", attrs...)
}

// handshakeFailureReason says why a connection's handshake failed, or nothing when
// the client simply went away before signing in.
func handshakeFailureReason(failure error) string {
	var authErr *ssh.ServerAuthError
	switch {
	case failure == nil, errors.Is(failure, io.EOF):
		return ""
	case strings.Contains(failure.Error(), "too many authentication failures"):
		return "too many authentication failures"
	case strings.Contains(failure.Error(), "too many authentication attempts"):
		return "too many authentication attempts"
	case errors.As(failure, &authErr):
		return ""
	default:
		return failure.Error()
	}
}

// remoteOf is the address of the client a gliderlabs context belongs to.
func remoteOf(ctx gliderlabs.Context) string {
	if state := connStateFrom(ctx); state != nil {
		return state.remote
	}
	if addr := ctx.RemoteAddr(); addr != nil {
		return addr.String()
	}
	return ""
}
