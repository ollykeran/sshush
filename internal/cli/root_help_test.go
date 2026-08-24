package cli

import "testing"

// TestRootHelp_ListsEditCommand guards against the edit subcommand silently
// disappearing from top-level help (e.g. if it's dropped from registerCommands).
func TestRootHelp_ListsEditCommand(t *testing.T) {
	root := NewRootCommand()
	registerCommands(root)

	ok := false
	for _, c := range root.Commands() {
		if c.Name() == "edit" {
			ok = true
			if c.Short == "" {
				t.Error("edit command should have a non-empty Short description")
			}
			break
		}
	}
	if !ok {
		t.Fatal("edit command not registered on root command")
	}
}

// TestEditCommand_HasLongAndExample guards against the edit subcommand's own
// --help output losing its detailed usage information.
func TestEditCommand_HasLongAndExample(t *testing.T) {
	cmd := newEditCommand()
	if cmd.Long == "" {
		t.Error("edit command should have a non-empty Long description")
	}
	if cmd.Example == "" {
		t.Error("edit command should have a non-empty Example")
	}
}
