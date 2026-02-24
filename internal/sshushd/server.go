package sshushd

import (
	"crypto/subtle"
	"fmt"
	"io"
	"net"

	"github.com/gliderlabs/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

type Server struct {
	Addr        string
	Keyring     sshagent.Agent
	HostKeyPath string
}

func checkAlreadyRunning(socketPath string) bool {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (s *Server) ListenAndServe() error {
	if checkAlreadyRunning(s.Addr) {
		return fmt.Errorf("agent already running at %s", s.Addr)
	}
	opts := []ssh.Option{
		ssh.PublicKeyAuth(s.publicKeyAuth),
	}
	if s.HostKeyPath != "" {
		opts = append(opts, ssh.HostKeyFile(s.HostKeyPath))
	}
	return ssh.ListenAndServe(s.Addr, s.handleSession, opts...)
}

func (s *Server) publicKeyAuth(ctx ssh.Context, key ssh.PublicKey) bool {
	keys, err := s.Keyring.List()
	if err != nil {
		return false
	}
	clientBlob := key.Marshal()
	for _, k := range keys {
		if len(k.Blob) == len(clientBlob) && subtle.ConstantTimeCompare(k.Blob, clientBlob) == 1 {
			return true
		}
	}
	return false
}

func (s *Server) handleSession(sess ssh.Session) {
	io.WriteString(sess, "sshush session (authorized by key)\n")
}
