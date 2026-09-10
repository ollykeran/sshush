package server

import (
	"crypto/subtle"
	"net"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// AgentAuth implements AuthKeySource using keys listed by an SSH agent.
type AgentAuth struct {
	Agent sshagent.Agent
}

// Authorized returns true if the given public key is one of the keys in the agent.
func (a *AgentAuth) Authorized(key ssh.PublicKey) bool {
	keys, err := a.Agent.List()
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

// SocketAuth implements AuthKeySource by asking the agent at SocketPath, dialling
// it fresh for each check.
//
// A connection held for the life of the server would be the obvious alternative,
// but the agent outlives nothing: `sshush reload`, `stop`/`start`, or a crash all
// replace it, and a server holding the old connection would refuse every key from
// then on with no way back but a restart. Dialling per check also means the server
// can start before the agent does — it authorizes nobody until the agent is up,
// then works without being touched.
//
// An authorization happens once per SSH connection, so the extra dial costs
// nothing worth measuring.
type SocketAuth struct {
	SocketPath string
}

// Authorized returns true if the agent currently lists the given public key. It
// returns false, rather than an error, when the agent cannot be reached: an
// unreachable agent authorizes nobody.
func (s *SocketAuth) Authorized(key ssh.PublicKey) bool {
	if s.SocketPath == "" {
		return false
	}
	conn, err := net.Dial("unix", s.SocketPath)
	if err != nil {
		return false
	}
	defer conn.Close()
	agentAuth := AgentAuth{Agent: sshagent.NewClient(conn)}
	return agentAuth.Authorized(key)
}
