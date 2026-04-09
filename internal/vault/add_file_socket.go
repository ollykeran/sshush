package vault

import "github.com/ollykeran/sshush/internal/agent"

// AddPrivateKeyFileToSocket adds the private key at path to the agent at socketPath.
// If the agent is a vault backend, uses add-key-opts with vaultAutoload; otherwise
// standard agent Add (vaultAutoload is ignored).
func AddPrivateKeyFileToSocket(socketPath, path string, vaultAutoload bool) error {
	mode, live := agent.LiveBackendMode(socketPath)
	if live && mode == "vault" {
		payload, err := BuildAddKeyOptsPayload(path, vaultAutoload)
		if err != nil {
			return err
		}
		_, err = agent.CallExtension(socketPath, ExtensionAddKeyOpts, payload)
		return err
	}
	return agent.AddKeyToSocketFromPath(socketPath, path)
}
