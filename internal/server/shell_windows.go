package server

import (
	"io"
	"os/exec"
	"time"

	gliderlabs "github.com/gliderlabs/ssh"
)

// shellKillGrace is how long the server waits for sessions on shutdown. There
// are none to wait for on Windows.
const shellKillGrace = 0 * time.Second

// loginShell names the shell sessions would run, for the log.
func loginShell(configured string) string { return configured }

// handleSession refuses every session: running a shell for a client needs a pty
// and process groups, which this server only knows how to do on Unix.
func (s *Server) handleSession(sess gliderlabs.Session) {
	s.logger().Error("session refused", "user", sess.User(), "remote", remoteOf(sess.Context()), "err", "sessions are not supported on Windows")
	_, _ = io.WriteString(sess.Stderr(), "sshush: sessions are not supported on Windows\n")
	_ = sess.Exit(1)
}

// ResolveShell finds a configured shell the way a command line would — a path as
// given, a bare name on PATH — and returns its path.
func ResolveShell(name string) (string, error) {
	return exec.LookPath(name)
}
