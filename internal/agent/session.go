package agent

import (
	"errors"
	"fmt"
	"net"
	"sync/atomic"

	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// Session is a single open connection to an SSH agent's Unix socket, and the
// only way sshush reaches a running agent. Every operation travels over the one
// connection the Session owns, so a command needing several operations pays for
// one dial rather than one per call.
//
// A Session must not be copied; always use *Session. Callers must Close it: the
// underlying agent client runs a background goroutine that Close stops, so a
// leaked Session leaks a goroutine as well as a file descriptor.
//
// Agent state — a vault's master key and session-load sets, a keyring's lock
// state — lives in the agent process and is shared by every connection it
// serves. How many Sessions a caller opens therefore has no bearing on what the
// agent reports.
type Session struct {
	conn       net.Conn
	client     sshagent.ExtendedAgent
	socketPath string
	closed     atomic.Bool
}

// Open dials the SSH agent listening on socketPath. The returned Session owns
// the connection and must be closed. A failure to dial is how callers learn the
// agent is not running.
func Open(socketPath string) (*Session, error) {
	if socketPath == "" {
		return nil, errors.New("agent: dial socket: no socket path")
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("agent: dial socket %s: %w", socketPath, err)
	}
	return &Session{
		conn:       conn,
		client:     sshagent.NewClient(conn),
		socketPath: socketPath,
	}, nil
}

// Close closes the connection to the agent. Close is idempotent; calls after the
// first return nil.
func (s *Session) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	return s.conn.Close()
}

// SocketPath returns the socket path this Session was opened with, unmodified.
func (s *Session) SocketPath() string { return s.socketPath }

// The methods below return the agent client's error unmodified, except Lock and
// Unlock, which prefer [ExtensionOp] so the reason survives. Any caller that
// still matches on the agent's own error text depends on this: wrapping here
// would silently change user-facing messages.

// List returns the keys the agent currently holds.
func (s *Session) List() ([]*sshagent.Key, error) { return s.client.List() }

// Add adds a private key to the agent.
func (s *Session) Add(key sshagent.AddedKey) error { return s.client.Add(key) }

// Remove removes the key matching key from the agent.
func (s *Session) Remove(key ssh.PublicKey) error { return s.client.Remove(key) }

// Lock locks the agent with passphrase. It prefers [ExtensionOp] so a failure
// names its reason — [ErrVaultLocked] for an agent already locked — and falls
// back to the plain protocol against an agent that does not implement it, where
// every failure collapses to "agent: failure".
func (s *Session) Lock(passphrase []byte) error {
	if _, err := s.Op(OpLock, passphrase); !errors.Is(err, ErrOpUnsupported) {
		return err
	}
	return s.client.Lock(passphrase)
}

// Unlock unlocks the agent with passphrase, preferring [ExtensionOp] so a
// failure names its reason: [ErrNotLocked] for an agent that was not locked,
// [ErrWrongPassphrase] for a bad passphrase. See [Session.Lock] for the
// fallback behaviour.
func (s *Session) Unlock(passphrase []byte) error {
	if _, err := s.Op(OpUnlock, passphrase); !errors.Is(err, ErrOpUnsupported) {
		return err
	}
	return s.client.Unlock(passphrase)
}

// Sign signs data with the private key the agent holds for key.
func (s *Session) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	return s.client.Sign(key, data)
}

// Extension calls an agent protocol extension. Extension names belong to the
// package that defines them — see internal/vault, which package agent cannot
// import — so this takes the name as given. An agent that does not implement the
// extension returns [sshagent.ErrExtensionUnsupported].
func (s *Session) Extension(extensionType string, contents []byte) ([]byte, error) {
	return s.client.Extension(extensionType, contents)
}

// AddKeyFromPath reads the private key at path and adds it to the agent,
// registering the fingerprint-to-filepath mapping for later lookup. Parse errors
// are returned unwrapped so callers can match [openssh.ErrEncryptedPrivateKey].
func (s *Session) AddKeyFromPath(path string) error {
	return AddKeyFromPath(s.client, path)
}

// RemoveByFingerprint removes the key with the given SHA256 fingerprint and
// reports whether one was removed. A fingerprint the agent does not hold returns
// (false, nil): callers distinguish "nothing to remove" from a failure.
func (s *Session) RemoveByFingerprint(fingerprint string) (bool, error) {
	keys, err := s.client.List()
	if err != nil {
		return false, fmt.Errorf("agent: list keys: %w", err)
	}
	for _, key := range keys {
		if ssh.FingerprintSHA256(key) == fingerprint {
			if err := s.client.Remove(key); err != nil {
				return false, fmt.Errorf("agent: remove key: %w", err)
			}
			return true, nil
		}
	}
	return false, nil
}
