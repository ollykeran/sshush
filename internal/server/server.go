package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"

	gliderlabs "github.com/gliderlabs/ssh"
	"golang.org/x/crypto/ssh"
)

// AuthKeySource provides authorized-key checks for the SSH server.
// Implementations must use constant-time comparison when comparing keys.
type AuthKeySource interface {
	Authorized(key ssh.PublicKey) bool
}

// Server is a TCP SSH server that authenticates by public key and serves a simple session message.
// It does not depend on config or CLI; all data is passed via struct fields.
type Server struct {
	ListenAddr  string
	AuthKeys    AuthKeySource
	HostKeyPath string
}

// ListenAndServe starts the SSH server on s.ListenAddr. It does not return until the server exits.
// If HostKeyPath is set, that file is used; otherwise an ephemeral in-memory key is used for this process.
func (s *Server) ListenAndServe() error {
	opts := []gliderlabs.Option{
		gliderlabs.PublicKeyAuth(s.publicKeyAuth),
	}
	if s.HostKeyPath != "" {
		opts = append(opts, gliderlabs.HostKeyFile(s.HostKeyPath))
	} else {
		pem, err := generateHostKeyPEM()
		if err != nil {
			return fmt.Errorf("generate host key: %w", err)
		}
		opts = append(opts, gliderlabs.HostKeyPEM(pem))
	}
	return gliderlabs.ListenAndServe(s.ListenAddr, s.handleSession, opts...)
}

func (s *Server) publicKeyAuth(ctx gliderlabs.Context, key gliderlabs.PublicKey) bool {
	return s.AuthKeys.Authorized(key)
}

func (s *Server) handleSession(sess gliderlabs.Session) {
	io.WriteString(sess, "sshush session (authorized by key)\n")
}

func generateHostKeyPEM() ([]byte, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(block), nil
}
