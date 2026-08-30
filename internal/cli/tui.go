package cli

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/theme"
	"github.com/ollykeran/sshush/internal/tui"
	"github.com/spf13/cobra"
)

func newTUICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Start the sshush TUI",
		Args:  argsNoneOrHelp,
		RunE:  runTUI,
	}
}

func runTUI(cmd *cobra.Command, _ []string) error {
	zone.NewGlobal()
	defer zone.Close()

	socketPath, _ := getSocketPath()
	configPath := ""
	if p, err := runtime.ResolveConfigPath(cmd); err == nil {
		configPath = p
	}
	th := theme.DefaultTheme()
	if configPath != "" {
		th = config.LoadThemeFromPath(configPath)
	}

	mode, vaultPath := effectiveTUIMode(env.Config)
	m := tui.NewTUI(configPath, socketPath, th, mode, vaultPath)
	_, err := tea.NewProgram(m).Run()
	if err != nil {
		return fmt.Errorf("cli: run tui: %w", err)
	}
	return nil
}

// effectiveTUIMode returns the agent backend mode and vault path the TUI should use.
// [agent].type is a single enum, so AgentBackendMode/VaultPathForAgent already return
// "keys"/"" for both "keys" and "external" — this just guards the nil-config case.
func effectiveTUIMode(cfg *config.Config) (mode, vaultPath string) {
	if cfg == nil {
		return "keys", ""
	}
	return cfg.AgentBackendMode(), cfg.VaultPathForAgent()
}
