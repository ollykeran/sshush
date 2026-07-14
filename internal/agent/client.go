package agent

import (
	"fmt"
	"net"

	sshagent "golang.org/x/crypto/ssh/agent"
)

// ListKeysFromSocket connects to an SSH agent socket and lists keys.
func ListKeysFromSocket(socketPath string) ([]*sshagent.Key, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("agent: dial socket %s: %w", socketPath, err)
	}
	defer conn.Close()
	return sshagent.NewClient(conn).List()
}
