package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	gliderlabs "github.com/gliderlabs/ssh"
	"golang.org/x/crypto/ssh"
)

// discardLogger stands in for a nil Server.Log.
var discardLogger = slog.New(slog.DiscardHandler)

func (s *Server) logger() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return discardLogger
}

// logf logs a formatted record at level, not formatting it at all when the level
// is off.
func (s *Server) logf(level slog.Level, format string, args ...any) {
	l := s.logger()
	if !l.Enabled(context.Background(), level) {
		return
	}
	l.Log(context.Background(), level, fmt.Sprintf(format, args...))
}

type connStateKey struct{}

// connState is what the log knows about one client connection, gathered from the
// separate callbacks gliderlabs and x/crypto make for it.
type connState struct {
	remote    string // "203.0.113.5 port 50022", the way sshd writes an address
	local     string
	remoteKey string // the remote address as given, for finding this state again
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
		remote:    describeAddr(conn.RemoteAddr()),
		local:     describeAddr(conn.LocalAddr()),
		remoteKey: conn.RemoteAddr().String(),
		opened:    time.Now(),
	}
	ctx.SetValue(connStateKey{}, state)
	s.pending.Store(state.remoteKey, state)
	s.logf(slog.LevelInfo, "Connection from %s on %s", state.remote, state.local)
	return &loggedConn{Conn: conn, server: s, state: state}
}

// onHandshakeFailed notes why a connection's handshake failed, for the line
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

// logAuth logs one authentication attempt the way sshd does: accepted or failed,
// by which method, for whom, from where, with the key's fingerprint for publickey
// and the methods still on offer after a failure. A "none" attempt is a client
// asking what it may use, so it logs the answer.
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

	detail := ""
	if method == "publickey" && key != nil {
		detail = ": " + keyTypeLabel(key.Type()) + " " + ssh.FingerprintSHA256(key)
	}
	switch {
	case err == nil:
		s.logf(slog.LevelInfo, "Accepted %s for %s from %s ssh2%s", method, user, state.remote, detail)
	case method == "none":
		s.logf(slog.LevelInfo, "Authentication methods offered to %s from %s: %s", user, state.remote, s.offeredMethods())
	default:
		reason := refusal
		if reason == "" {
			reason = authFailureReason(err)
		}
		if reason != "" {
			reason = " (" + reason + ")"
		}
		s.logf(slog.LevelInfo, "Failed %s for %s from %s ssh2%s%s; methods offered: %s",
			method, user, state.remote, detail, reason, s.offeredMethods())
		if method == "password" && lockedOut != "" {
			s.logLockout(lockedOut)
		}
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
// comma-separated form the protocol and sshd use.
func (s *Server) offeredMethods() string {
	if s.Passwords != nil {
		return "publickey,password"
	}
	return "publickey"
}

// logLockout reports that host has just been locked out of password
// authentication. It follows the log line of the failure that did it.
func (s *Server) logLockout(host string) {
	s.logf(slog.LevelWarn, "Locking %s out of password authentication for %v after %d failed passwords",
		host, passwordLockout, passwordLockoutFailures)
}

// onSessionRequest logs and refuses subsystem requests, which is how sftp and so
// plain scp ask to start. Shells and commands are let through, to be logged by
// the session itself. Refusing here answers the request with the same failure an
// unhandled subsystem gets, so clients see no difference.
func (s *Server) onSessionRequest(sess gliderlabs.Session, requestType string) bool {
	if requestType != "subsystem" {
		return true
	}
	s.logf(slog.LevelInfo, "Refused subsystem request for %s by user %s from %s: not supported",
		sess.Subsystem(), sess.User(), remoteOf(sess.Context()))
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
		c.server.pending.Delete(c.state.remoteKey)
		c.server.logDisconnect(c.state)
	})
	return err
}

// logDisconnect logs how a connection ended, in sshd's words: after signing in,
// or before it ("[preauth]") — and then whether the client gave up, ran out of
// attempts, or never managed a handshake at all.
func (s *Server) logDisconnect(state *connState) {
	state.mu.Lock()
	user, authenticated, failure := state.user, state.authenticated, state.failure
	state.mu.Unlock()

	var authErr *ssh.ServerAuthError
	switch {
	case authenticated:
		s.logf(slog.LevelInfo, "Disconnected from user %s %s (connected %v)",
			user, state.remote, time.Since(state.opened).Round(time.Millisecond))
	case failure != nil && strings.Contains(failure.Error(), "too many authentication failures"):
		s.logf(slog.LevelInfo, "Disconnecting authenticating user %s %s: Too many authentication failures [preauth]", user, state.remote)
	case failure != nil && strings.Contains(failure.Error(), "too many authentication attempts"):
		s.logf(slog.LevelInfo, "Disconnecting authenticating user %s %s: Too many authentication attempts [preauth]", user, state.remote)
	case user != "":
		s.logf(slog.LevelInfo, "Connection closed by authenticating user %s %s [preauth]", user, state.remote)
	case failure == nil, errors.Is(failure, io.EOF), errors.As(failure, &authErr):
		s.logf(slog.LevelInfo, "Connection closed by %s [preauth]", state.remote)
	default:
		s.logf(slog.LevelInfo, "Handshake with %s failed: %v [preauth]", state.remote, failure)
	}
}

// remoteOf describes the client a gliderlabs context belongs to.
func remoteOf(ctx gliderlabs.Context) string {
	if state := connStateFrom(ctx); state != nil {
		return state.remote
	}
	return describeAddr(ctx.RemoteAddr())
}

// describeAddr writes a TCP address the way sshd logs one: "203.0.113.5 port 50022".
func describeAddr(addr net.Addr) string {
	if addr == nil {
		return "unknown address"
	}
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host + " port " + port
}

// keyTypeLabel names a public key type the way sshd does in its log: ED25519,
// RSA, ECDSA, with -SK for security keys and -CERT for certificates.
func keyTypeLabel(keyType string) string {
	var label string
	switch {
	case strings.HasPrefix(keyType, "sk-ssh-ed25519"):
		label = "ED25519-SK"
	case strings.HasPrefix(keyType, "sk-ecdsa-"):
		label = "ECDSA-SK"
	case strings.HasPrefix(keyType, "ssh-ed25519"):
		label = "ED25519"
	case strings.HasPrefix(keyType, "ssh-rsa"):
		label = "RSA"
	case strings.HasPrefix(keyType, "ecdsa-sha2-"):
		label = "ECDSA"
	case strings.HasPrefix(keyType, "ssh-dss"):
		label = "DSA"
	default:
		return keyType
	}
	if strings.Contains(keyType, "-cert-") {
		label += "-CERT"
	}
	return label
}
