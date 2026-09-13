// Package agent implements both sides of the SSH agent protocol for sshush.
//
// On the server side it serves the protocol over a Unix socket ([ListenAndServe])
// and loads keys from paths on disk into a keyring.
//
// On the client side [Session] is the only way to reach a running agent: it owns
// one connection and must be closed. Agent state is held by the agent process
// and shared across every connection it serves, so the number of Sessions a
// caller opens does not affect what the agent reports.
//
// Extension names belong to the packages that define them — see internal/vault,
// which imports this package and so cannot be imported back.
//
// It uses golang.org/x/crypto/ssh/agent for the protocol and keyring.
package agent
