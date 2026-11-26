//go:build !windows

package pageant

import (
	"io"

	sshagent "golang.org/x/crypto/ssh/agent"
)

// OwnerPID is never found off Windows, where there is no Pageant window.
func OwnerPID() (uint32, bool) { return 0, false }

// Serve does nothing off Windows: Pageant is a Windows thing.
func Serve(sshagent.Agent, io.Writer) Status { return Off }
