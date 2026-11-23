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

	persisted, _ := platform.PersistedEnv("SSH_AUTH_SOCK")
	report := selftest.Run(socketPath, os.Getenv("SSH_AUTH_SOCK"), persisted)

	out := style.NewOutput()
	for _, c := range report.Checks {
		out.Add(style.Focus(fmt.Sprintf("%-8s", c.Name+":")) + styleFor(c.Status)(c.Detail))
		if c.Hint != "" {
			out.Add(style.Focus("        ") + style.Err(c.Hint))
		}
	}
	// Checksum only after every check passed as far as signing.
	if last := report.Checks[len(report.Checks)-1]; last.Name != "sign" || last.Status != selftest.OK {
		out.Print()
		return nil
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
