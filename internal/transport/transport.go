// Package transport opens the connections the SSH agent protocol travels over.
// Everywhere that is a Unix domain socket at a filesystem path; on Windows it is
// also a named pipe (\\.\pipe\name), which is what Win32-OpenSSH's ssh.exe and
// ssh-add.exe expect SSH_AUTH_SOCK to name. Callers pass the configured socket
// path as it is and never need to know which of the two it is.
package transport

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// ErrForeignPipe is returned by Dial when the named pipe at path exists but
// belongs to some other account on the machine. A pipe name is machine-wide, so
// whoever creates it first serves it, and what a client sends an agent includes
// the vault passphrase and private keys: Dial hangs up before anything is
// written rather than hand those over. It is not "nothing is running": the name
// is taken, so listening on it fails as well, and callers that go on to listen
// after a failed Dial should report this error instead of the one from Listen.
var ErrForeignPipe = errors.New("named pipe is served by another account, so sshush will not send secrets to it")

// Dial connects to the agent listening at path. On Windows it refuses a pipe
// that another account is serving, with an error that wraps [ErrForeignPipe].
func Dial(path string) (net.Conn, error) {
	if pipe, ok := pipePath(path); ok {
		return dialPipe(pipe)
	}
	return net.Dial("unix", path)
}

// Listen starts listening at path. For a socket it creates the parent directory
// (0700) and replaces whatever file is at path, so callers that care whether
// another agent is live there must Dial first. A named pipe has no file to
// replace: listening on a name that is already served is an error.
func Listen(path string) (net.Listener, error) {
	if pipe, ok := pipePath(path); ok {
		return listenPipe(pipe)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create socket directory %s: %w", dir, err)
	}
	_ = os.Remove(path)
	return net.Listen("unix", path)
}

// IsPipe reports whether path names a Windows named pipe rather than a socket file.
func IsPipe(path string) bool {
	_, ok := pipePath(path)
	return ok
}

// Abs returns path in the absolute form to hand to other processes (in
// SSH_AUTH_SOCK, say). A pipe name is already absolute and is only normalised.
func Abs(path string) (string, error) {
	if pipe, ok := pipePath(path); ok {
		return pipe, nil
	}
	return filepath.Abs(path)
}

// Remove deletes the socket file at path once its listener is done with it. A
// pipe goes away with its listener, so there is nothing to delete.
func Remove(path string) {
	if IsPipe(path) {
		return
	}
	_ = os.Remove(path)
}
