package cli

import (
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/style"
)

// windowsAgentDown is the error for an agent that took the connection and then
// did not answer, when cfg relays to Windows: the relay here is up and the
// agent behind it is not. It returns nil for any other config, and the caller
// then reports the failure its usual way.
func windowsAgentDown(cfg *config.Config) error {
	if cfg == nil || !cfg.RelaysToWindows() {
		return nil
	}
	return style.NewOutput().
		Error("the sshush agent on Windows is not answering").
		Info("Start it with 'sshush start', here or in PowerShell.").
		AsError()
}
