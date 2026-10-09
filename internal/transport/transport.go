// Package transport opens the connections the SSH agent protocol travels over.
// Everywhere that is a Unix domain socket at a filesystem path; on Windows it is
// also a named pipe (\\.\pipe\name), which is what Win32-OpenSSH's ssh.exe and
// ssh-add.exe expect SSH_AUTH_SOCK to name. Callers pass the configured socket
// path as it is and never need to know which of the two it is.
package transport

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// Dial connects to the agent listening at path.
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
