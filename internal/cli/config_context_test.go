package cli

import (
	"io"
	"testing"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/spf13/cobra"
)

// newConfiguredCommand returns a bare command carrying cfg, as the root pre-run
// would leave it, with output discarded.
func newConfiguredCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	withConfig(cmd, cfg)
	return cmd
}

// The root pre-run runs with the executing subcommand, so a config stored there
// must be what that subcommand's RunE reads.
func TestWithConfig_reachesSubcommandRunE(t *testing.T) {
	t.Parallel()
	want := &config.Config{SocketPath: "/tmp/ctx.sock"}
	var got *config.Config

	root := &cobra.Command{
		Use: "root",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			withConfig(cmd, want)
			return nil
		},
	}
	root.AddCommand(&cobra.Command{
		Use: "child",
		RunE: func(cmd *cobra.Command, _ []string) error {
			got = configFrom(cmd)
			return nil
		},
	})
	root.SetArgs([]string{"child"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got != want {
		t.Fatalf("configFrom in RunE: got %v, want %v", got, want)
	}
}

func TestConfigFrom_commandNeverExecuted(t *testing.T) {
	t.Parallel()
	if cfg := configFrom(&cobra.Command{}); cfg != nil {
		t.Fatalf("got %v, want nil", cfg)
	}
}

func TestWithConfig_nilClearsEarlierConfig(t *testing.T) {
	t.Parallel()
	cmd := newConfiguredCommand(&config.Config{SocketPath: "/tmp/old.sock"})
	withConfig(cmd, nil)
	if cfg := configFrom(cmd); cfg != nil {
		t.Fatalf("got %v, want nil", cfg)
	}
}

func TestConfigContext_commandsDoNotShareConfig(t *testing.T) {
	t.Parallel()
	a := newConfiguredCommand(&config.Config{SocketPath: "/tmp/a.sock"})
	b := newConfiguredCommand(&config.Config{SocketPath: "/tmp/b.sock"})
	if got := configFrom(a).SocketPath; got != "/tmp/a.sock" {
		t.Errorf("a: got %q", got)
	}
	if got := configFrom(b).SocketPath; got != "/tmp/b.sock" {
		t.Errorf("b: got %q", got)
	}
}
