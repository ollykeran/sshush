package cli

import (
	"os"
	"testing"

	"github.com/ollykeran/sshush/internal/kdf"
)

// TestMain weakens the Argon2id cost parameters for this package's tests.
// Several tests spin up a real vault via startTestVaultAgent, which pays the
// full production KDF cost on every Init/Unlock otherwise.
func TestMain(m *testing.M) {
	kdf.SetInsecureFastParamsForTesting()
	os.Exit(m.Run())
}
