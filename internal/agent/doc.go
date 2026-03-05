// Package agent implements the SSH agent: listing keys, loading keys from paths,
// and serving the agent protocol over a Unix socket. It uses golang.org/x/crypto/ssh/agent
// for the protocol and keyring.
package agent
