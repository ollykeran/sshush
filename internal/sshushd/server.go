package sshushd

import "github.com/ollykeran/sshush/internal/transport"

// checkAlreadyRunning returns true if something is already listening on the agent socket.
// Used only for the agent socket (daemon control flow), not for TCP listen addresses.
func checkAlreadyRunning(socketPath string) bool {
	conn, err := transport.Dial(socketPath)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
