package server

import (
	"os"
	"testing"

	"github.com/ollykeran/sshush/internal/kdf"
)

// TestMain weakens the Argon2id cost parameters for this package's tests, which
// create vaults and check passphrases against them; each would pay the full
// production KDF cost otherwise.
func TestMain(m *testing.M) {
	kdf.SetInsecureFastParamsForTesting()
	os.Exit(m.Run())
}
