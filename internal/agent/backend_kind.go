package agent

import (
	"errors"

	sshagent "golang.org/x/crypto/ssh/agent"
)

// extensionVaultLocked must match vault.ExtensionVaultLocked (package agent cannot import vault).
const extensionVaultLocked = "vault-locked"

// LiveBackendMode reports whether the agent at socketPath is a vault backend or a plain keyring.
// If the socket is reachable and responds to the vault-locked extension, it returns "vault", true.
// If the extension is unsupported (typical keyring / OpenSSH agent), it returns "keys", true.
// On dial or other errors, it returns "", false.
func LiveBackendMode(socketPath string) (mode string, reachable bool) {
	if socketPath == "" {
		return "", false
	}
	_, err := CallExtension(socketPath, extensionVaultLocked, nil)
	if err == nil {
		return "vault", true
	}
	if errors.Is(err, sshagent.ErrExtensionUnsupported) {
		return "keys", true
	}
	return "", false
}
