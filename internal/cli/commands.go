package cli

import (
	"github.com/spf13/cobra"
)

func registerCommands(root *cobra.Command) {
	root.AddCommand(newStartCommand())
	root.AddCommand(newStopCommand())
	root.AddCommand(newReloadCommand())
	root.AddCommand(newListCommand())
	root.AddCommand(newAddCommand())
	root.AddCommand(newRemoveCommand())
	root.AddCommand(newCreateCommand())
	root.AddCommand(newEditCommand())
	root.AddCommand(newExportCommand())
	root.AddCommand(newVersionCommand())
	root.AddCommand(newTUICommand())
	root.AddCommand(newFindCommand())
	// root.AddCommand(newLockCommand())
	// root.AddCommand(newUnlockCommand())
}
