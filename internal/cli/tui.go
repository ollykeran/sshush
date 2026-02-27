package cli

import (
	tea "charm.land/bubbletea/v2"
	"github.com/ollykeran/sshush/internal/tui"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
)

func newTUICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Start the sshush TUI",
		RunE:  runTUI,
	}
}

func runTUI(cmd *cobra.Command, _ []string) error {
	socketPath, _ := getSocketPath()
	configPath := ""
	if p, err := utils.ResolveConfigPath(cmd); err == nil {
		configPath = p
	}

	m := tui.NewRootModel(configPath, socketPath)
	_, err := tea.NewProgram(m).Run()
	return err
}
