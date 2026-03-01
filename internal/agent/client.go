package agent

import (
	"net"

	sshagent "golang.org/x/crypto/ssh/agent"
)

// ListKeysFromSocket connects to an SSH agent socket and lists keys.
func ListKeysFromSocket(socketPath string) ([]*sshagent.Key, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return sshagent.NewClient(conn).List()
}
