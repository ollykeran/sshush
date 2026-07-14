package vault

import (
	"fmt"

	"github.com/ollykeran/sshush/internal/agent"
)

// AddPrivateKeyFileToSocket adds the private key at path to the agent at socketPath.
// If the agent is a vault backend, uses add-key-opts with vaultAutoload; otherwise
// standard agent Add (vaultAutoload is ignored).
func AddPrivateKeyFileToSocket(socketPath, path string, vaultAutoload bool) error {
	mode, live := agent.LiveBackendMode(socketPath)
	if live && mode == "vault" {
		payload, err := BuildAddKeyOptsPayload(path, vaultAutoload)
		if err != nil {
			return fmt.Errorf("vault: build add-key-opts payload: %w", err)
		}
		_, err = agent.CallExtension(socketPath, ExtensionAddKeyOpts, payload)
		if err != nil {
			return fmt.Errorf("vault: call add-key-opts extension: %w", err)
		}
		return nil
	}
	if err := agent.AddKeyToSocketFromPath(socketPath, path); err != nil {
		return fmt.Errorf("vault: add key to socket: %w", err)
	}
	return nil
}
