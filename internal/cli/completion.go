package cli

import (
	"os"

	"github.com/spf13/cobra"
)

func newCompletionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh|fish]",
		Short: "Generate shell completion script",
		Long: `Generate shell completion script for sshush.

To load completions:

  Bash:
    source <(sshush completion bash)

  Zsh:
    source <(sshush completion zsh)

  Fish:
    sshush completion fish | source
`,
		ValidArgs: []string{"bash", "zsh", "fish"},
		Args:      cobra.ExactValidArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := cmd.Root()
			switch args[0] {
			case "bash":
				return root.GenBashCompletionV2(os.Stdout, true)
			case "zsh":
				return root.GenZshCompletion(os.Stdout)
			case "fish":
				return root.GenFishCompletion(os.Stdout, true)
			}
			return nil
		},
	}
}
