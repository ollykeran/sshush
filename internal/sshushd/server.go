package sshushd

import "net"

// checkAlreadyRunning returns true if something is already listening on the unix socket.
// Used only for the agent socket (daemon control flow), not for TCP listen addresses.
func checkAlreadyRunning(socketPath string) bool {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
