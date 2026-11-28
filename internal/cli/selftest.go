package cli

import (
	"fmt"
	"os"

	"github.com/ollykeran/sshush/internal/platform"
	"github.com/ollykeran/sshush/internal/selftest"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/version"
	"github.com/spf13/cobra"
)

func newSelftestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "selftest",
		Short:   "Test agent connectivity",
		Example: "sshush selftest\nsshush selftest --extended",
		Long: "Check that the SSH agent socket is reachable, can list keys, and can sign with a key.\n\n" +
			"With --extended, also start a temporary sshush SSH server on 127.0.0.1 and log in to it " +
			"with the agent's first key, proving the agent can complete a real SSH login. Nothing leaves this machine.",
		Args: nil,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				cmd.Usage()
				return style.NewOutput().Error("selftest takes no arguments").AsError()
			}
			return runSelftest(cmd, args)
		},
	}
	cmd.Flags().BoolP("extended", "x", false, "also log in over SSH to a temporary local sshush server")
	return cmd
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
	extended, _ := cmd.Flags().GetBool("extended")

	persisted, _ := platform.PersistedEnv("SSH_AUTH_SOCK")
	report := selftest.Run(socketPath, os.Getenv("SSH_AUTH_SOCK"), persisted)

	out := style.NewOutput()
	for _, c := range report.Checks {
		addCheck(out, c)
	}
	// The extended login and the checksum only after every check passed as far
	// as signing.
	if last := report.Checks[len(report.Checks)-1]; last.Name != "sign" || last.Status != selftest.OK {
		out.Print()
		return nil
	}
	if extended {
		addCheck(out, selftest.Extended(socketPath))
	}

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

// addCheck adds one check's line, and its hint if it has one, to out.
func addCheck(out *style.Output, c selftest.Check) {
	out.Add(style.Focus(fmt.Sprintf("%-8s", c.Name+":")) + styleFor(c.Status)(c.Detail))
	if c.Hint != "" {
		out.Add(style.Focus("        ") + style.Err(c.Hint))
	}
}

func styleFor(s selftest.Status) func(string) string {
	switch s {
	case selftest.OK:
		return style.Success
	case selftest.Warn:
		return style.Warn
	default:
		return style.Err
	}
}
