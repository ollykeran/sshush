package vaultops

import (
	"os"
	"testing"

	"github.com/ollykeran/sshush/internal/kdf"
)

// TestMain weakens the Argon2id cost parameters for this package's tests.
// Every verb here reaches a real vault agent, so each test pays Init and
// Unlock; the production defaults would make the suite unusably slow.
func TestMain(m *testing.M) {
	kdf.SetInsecureFastParamsForTesting()
	os.Exit(m.Run())
}
