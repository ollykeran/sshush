package server

import (
	"crypto/subtle"

	sshagent "golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh"
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
