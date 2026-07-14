package cli

import (
	"fmt"
	"net"
	"os"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/vault"
	"github.com/spf13/cobra"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func newSelftestCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "selftest",
		Short:   "Test agent connectivity",
		Example: "sshush selftest",
		Long:    "Check that the SSH agent socket is reachable, can list keys, and can sign with a key.",
		Args: nil,
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
	if env.Config == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}
	socketPath, err := getSocketPath()
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

	liveMode, liveOK := agent.LiveBackendMode(socketPath)
	if liveOK {
		out.Add(style.Focus("agent:  ") + style.Success(liveMode+"  ✓"))
	} else {
		out.Add(style.Focus("agent:  ") + style.Err("unreachable"))
	}

	if liveMode == "vault" {
		resp, extErr := agent.CallExtension(socketPath, vault.ExtensionVaultLocked, nil)
		if extErr == nil && len(resp) == 1 && resp[0] == 1 {
			out.Add(style.Focus("state:  ") + style.Err("locked"))
		} else {
			out.Add(style.Focus("state:  ") + style.Success("unlocked  ✓"))
		}
	} else if liveOK {
		out.Add(style.Focus("state:  ") + style.Success("ready  ✓"))
	}

	if _, err := os.Stat(socketPath); err != nil {
		out.Add(style.Focus("socket: ") + style.Err(fmt.Sprintf("%s  ✗", socketPath)))
		out.Add(style.Focus("        ") + style.Err("agent is not running (try sshush start)"))
		out.Print()
		return nil
	}
	out.Add(style.Focus("socket: ") + style.Success(fmt.Sprintf("%s  ✓", socketPath)))

	keys, err := agent.ListKeysFromSocket(socketPath)
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

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		out.Add(style.Focus("sign:   ") + style.Err(fmt.Sprintf("✗ %v", err)))
		out.Print()
		return nil
	}
	defer conn.Close()

	client := sshagent.NewClient(conn)
	_, err = client.Sign(keys[0], []byte("sshush-test"))
	if err != nil {
		out.Add(style.Focus("sign:   ") + style.Err(fmt.Sprintf("✗ %v", err)))
		out.Print()
		return nil
	}
	out.Add(style.Focus("sign:   ") + style.Success(fmt.Sprintf("%s  ✓", keys[0].Comment)))

	out.Print()
	return nil
}
