package server

import (
	"fmt"
	"net"

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
	// Ready, if set, is called once the TCP listener is accepting
	// connections, before ListenAndServe blocks serving them.
	Ready func()

	// shellPath is Shell resolved to a path, set before any session starts.
	shellPath string
}

// ListenAndServe starts the SSH server on s.ListenAddr. It does not return until the server exits.
// If HostKeyPath is set, that file is used, and a host key is generated there when the file
// does not exist yet; otherwise an ephemeral in-memory key is used for this process.
// A Shell that cannot be found is an error before anything listens.
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
		serveOnlySessions,
	}
	if s.Passwords != nil {
		guard := newPasswordGuard(s.Passwords)
		opts = append(opts, gliderlabs.PasswordAuth(func(ctx gliderlabs.Context, password string) bool {
			return guard.check(ctx, ctx.RemoteAddr(), []byte(password))
		}))
	}
	if s.HostKeyPath != "" {
		if _, err := EnsureHostKey(s.HostKeyPath); err != nil {
			return err
		}
		opts = append(opts, gliderlabs.HostKeyFile(s.HostKeyPath))
	} else {
		pem, err := generateHostKeyPEM()
		if err != nil {
			return fmt.Errorf("server: generate host key: %w", err)
		}
		opts = append(opts, gliderlabs.HostKeyPEM(pem))
	}
	ln, err := net.Listen("tcp", s.ListenAddr)
	if err != nil {
		return fmt.Errorf("server: listen %s: %w", s.ListenAddr, err)
	}
	if s.Ready != nil {
		s.Ready()
	}
	if err := gliderlabs.Serve(ln, s.handleSession, opts...); err != nil {
		return fmt.Errorf("server: listen %s: %w", s.ListenAddr, err)
	}
	return nil
}

func (s *Server) publicKeyAuth(ctx gliderlabs.Context, key gliderlabs.PublicKey) bool {
	return s.AuthKeys.Authorized(key)
}
