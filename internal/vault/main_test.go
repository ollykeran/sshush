package vault

import (
	"os"
	"testing"

	"github.com/ollykeran/sshush/internal/kdf"
)

// TestMain weakens the Argon2id cost parameters for this package's tests.
// Vault tests call Init/Unlock/EnableRecoveryWithPassphrase repeatedly, and
// each call pays the full production KDF cost otherwise — this keeps the
// suite fast without touching the real (secure) defaults used in production.
func TestMain(m *testing.M) {
	kdf.SetInsecureFastParamsForTesting()
	os.Exit(m.Run())
}
