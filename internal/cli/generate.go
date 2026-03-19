package cli

import (
	"path/filepath"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
)

func newGenerateCommand() *cobra.Command {
	gen := &cobra.Command{
		Use:   "generate",
		Short: "Generate files (e.g. default config)",
	}
	gen.AddCommand(newGenerateConfigCommand())
	return gen
}

func newGenerateConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "config [path]",
		Short:   "Write default config TOML to a file",
		Long:    "Renders the same default config as first-run setup. With no path, writes to ~/.config/sshush/config.toml.",
		Example: "sshush generate config\nsshush generate config ./sshush.toml\nsshush generate config --force",
		Args:    cobra.MaximumNArgs(1),
		RunE:    runGenerateConfig,
	}
	cmd.Flags().BoolP("force", "f", false, "overwrite the file if it already exists")
	return cmd
}

func runGenerateConfig(cmd *cobra.Command, args []string) error {
	outPath := config.StandardConfigFile()
	if len(args) > 0 {
		outPath = utils.ExpandHomeDirectory(args[0])
	}
	force, err := cmd.Flags().GetBool("force")
	if err != nil {
		return err
	}
	if err := config.WriteDefaultConfigFile(outPath, force); err != nil {
		return err
	}
	absPath := outPath
	if p, err := filepath.Abs(outPath); err == nil {
		absPath = p
	}
	style.NewOutput().
		InfoBold("Default sshush configuration (TOML)").
		Info(utils.DisplayPath(absPath)).
		Print()
	return nil
}
