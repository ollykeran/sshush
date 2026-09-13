package cli

import (
	"context"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/spf13/cobra"
)

// configKey is the context key under which the root pre-run stores the merged config.
type configKey struct{}

// withConfig stores cfg on cmd's context, so its RunE can read it with configFrom.
// Tests build a command with a config this way instead of mutating shared state.
func withConfig(cmd *cobra.Command, cfg *config.Config) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	cmd.SetContext(context.WithValue(ctx, configKey{}, cfg))
}

// configFrom returns the merged config carried by cmd, or nil when none was loaded
// (generate config, theme without a config file, or a command never run through root).
func configFrom(cmd *cobra.Command) *config.Config {
	ctx := cmd.Context()
	if ctx == nil {
		return nil
	}
	cfg, _ := ctx.Value(configKey{}).(*config.Config)
	return cfg
}
