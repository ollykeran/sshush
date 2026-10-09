package cli

import (
	"fmt"
	"os"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/transport"
	"github.com/ollykeran/sshush/internal/version"
	"github.com/spf13/cobra"
)

func newSelftestCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "selftest",
		Short:   "Test agent connectivity",
		Example: "sshush selftest",
		Long:    "Check that the SSH agent socket is reachable, can list keys, and can sign with a key.",
		Args:    nil,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				cmd.Usage()
				return style.NewOutput().Error("selftest takes no arguments").AsError()
			}
			return runSelftest(cmd, args)
		},
	}
}

func runSelftest(cmd *cobra.Command, _ []string) error {
	cfg := configFrom(cmd)
	if cfg == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}
	socketPath, err := getSocketPath(cfg)
	if err != nil {
		return fmt.Errorf("cli: get socket path: %w", err)
	}

	out := style.NewOutput()

	authSock := os.Getenv("SSH_AUTH_SOCK")
	if authSock == "" {
		out.Add(style.Focus("env:    ") + style.Err("SSH_AUTH_SOCK not set"))
	} else if authSock != socketPath {
		out.Add(style.Focus("env:    ") + style.Err(fmt.Sprintf("SSH_AUTH_SOCK=%s (differs from socket)", authSock)))
	} else {
		out.Add(style.Focus("env:    ") + style.Success(fmt.Sprintf("SSH_AUTH_SOCK=%s  ✓", authSock)))
	}

	session, sessionErr := agent.Open(socketPath)
	if sessionErr == nil {
		defer session.Close()
	}
	var backend agent.Backend
	liveOK := false
	if sessionErr == nil {
		if b, err := session.Backend(); err == nil {
			backend, liveOK = b, true
		}
	}
	if liveOK {
		out.Add(style.Focus("agent:  ") + style.Success(backend.Mode+"  ✓"))
	} else {
		out.Add(style.Focus("agent:  ") + style.Err("unreachable"))
	}

	if backend.Mode == "vault" {
		if backend.VaultLocked {
			out.Add(style.Focus("state:  ") + style.Err("locked"))
		} else {
			out.Add(style.Focus("state:  ") + style.Success("unlocked  ✓"))
		}
	} else if liveOK {
		out.Add(style.Focus("state:  ") + style.Success("ready  ✓"))
	}

	// A named pipe is not a file to stat: it exists exactly when it can be dialled.
	socketMissing := sessionErr != nil
	if !transport.IsPipe(socketPath) {
		_, statErr := os.Stat(socketPath)
		socketMissing = statErr != nil
	}
	if socketMissing {
		out.Add(style.Focus("socket: ") + style.Err(fmt.Sprintf("%s  ✗", socketPath)))
		out.Add(style.Focus("        ") + style.Err("agent is not running (try sshush start)"))
		out.Print()
		return nil
	}
	out.Add(style.Focus("socket: ") + style.Success(fmt.Sprintf("%s  ✓", socketPath)))

	// The socket file can exist while the dial failed (a stale socket), so the
	// os.Stat check above does not guarantee a session.
	if sessionErr != nil {
		out.Add(style.Focus("list:   ") + style.Err(fmt.Sprintf("✗ %v", sessionErr)))
		out.Print()
		return nil
	}
	keys, err := session.List()
	if err != nil {
		out.Add(style.Focus("list:   ") + style.Err(fmt.Sprintf("✗ %v", err)))
		out.Print()
		return nil
	}
	if len(keys) == 0 {
		out.Add(style.Focus("list:   ") + style.Warn("no keys loaded"))
		out.Print()
		return nil
	}
	out.Add(style.Focus("list:   ") + style.Success(fmt.Sprintf("%d key(s) loaded  ✓", len(keys))))

	if _, err := session.Sign(keys[0], []byte("sshush-test")); err != nil {
		out.Add(style.Focus("sign:   ") + style.Err(fmt.Sprintf("✗ %v", err)))
		out.Print()
		return nil
	}
	out.Add(style.Focus("sign:   ") + style.Success(fmt.Sprintf("%s  ✓", keys[0].Comment)))

	integrity, integErr := version.VerifyBinaryChecksum()
	switch {
	case integErr != nil:
		out.Add(style.Focus("checksum: ") + style.Err(fmt.Sprintf("✗ %v", integErr)))
	case integrity.Verified:
		out.Add(style.Focus("checksum: ") + style.Success(integrity.Message+"  ✓"))
	default:
		out.Add(style.Focus("checksum: ") + style.Warn(integrity.Message))
	}

	out.Print()
	return nil
}
