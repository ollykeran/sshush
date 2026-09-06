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

// Server is a TCP SSH server that authenticates by public key and serves an interactive
// shell on a pty to each session.
// It does not depend on config or CLI; all data is passed via struct fields.
type Server struct {
	ListenAddr string
	AuthKeys   AuthKeySource
	// HostKeyPath is the server's host key file. It is created if missing.
	// Empty means an ephemeral key for this process, which every client will see
	// as a host key change, so callers that outlive one run should always set it.
	HostKeyPath string
	// Ready, if set, is called once the TCP listener is accepting
	// connections, before ListenAndServe blocks serving them.
	Ready func()
}

// ListenAndServe starts the SSH server on s.ListenAddr. It does not return until the server exits.
// If HostKeyPath is set, that file is used, and a host key is generated there when the file
// does not exist yet; otherwise an ephemeral in-memory key is used for this process.
func (s *Server) ListenAndServe() error {
	opts := []gliderlabs.Option{
		gliderlabs.PublicKeyAuth(s.publicKeyAuth),
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
