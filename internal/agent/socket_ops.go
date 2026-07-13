package agent

import (
	"fmt"
	"net"

	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func withSocketClient(socketPath string, fn func(client sshagent.ExtendedAgent) error) error {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	return fn(sshagent.NewClient(conn))
}

// CallExtension calls the agent extension and returns the response or error.
func CallExtension(socketPath, extensionType string, payload []byte) ([]byte, error) {
	var result []byte
	err := withSocketClient(socketPath, func(client sshagent.ExtendedAgent) error {
		var e error
		result, e = client.Extension(extensionType, payload)
		return e
	})
	return result, err
}

// AddKeyToSocketFromPath adds a key file to the running agent socket.
func AddKeyToSocketFromPath(socketPath, path string) error {
	return withSocketClient(socketPath, func(client sshagent.ExtendedAgent) error {
		return AddKeyFromPath(client, path)
	})
}

// RemoveKeyFromSocketByFingerprint removes a key by fingerprint.
// It returns true when a key was removed.
func RemoveKeyFromSocketByFingerprint(socketPath, fingerprint string) (bool, error) {
	removed := false
	err := withSocketClient(socketPath, func(client sshagent.ExtendedAgent) error {
		keys, err := client.List()
		if err != nil {
			return err
		}
		for _, key := range keys {
			if ssh.FingerprintSHA256(key) == fingerprint {
				if err := client.Remove(key); err != nil {
					return fmt.Errorf("remove key: %w", err)
				}
				removed = true
				return nil
			}
		}
		return nil
	})
	return removed, err
}

// LockSocket locks the running agent.
func LockSocket(socketPath string, passphrase []byte) error {
	return withSocketClient(socketPath, func(client sshagent.ExtendedAgent) error {
		return client.Lock(passphrase)
	})
}

// UnlockSocket unlocks the running agent.
func UnlockSocket(socketPath string, passphrase []byte) error {
	return withSocketClient(socketPath, func(client sshagent.ExtendedAgent) error {
		return client.Unlock(passphrase)
	})
}
