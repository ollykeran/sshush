package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
)

func newFindCommand() *cobra.Command {
	var noDefaults, recursive bool
	cmd := &cobra.Command{
		Use:     "find",
		Short:   "Find Private OpenSSH keys",
		Long:    "Find Private OpenSSH keys in given paths, including current working directory and ~/.ssh",
		Example: "sshush find /some/path",
		RunE: func(c *cobra.Command, args []string) error {
			return runFind(noDefaults, recursive, args...)
		},
	}
	cmd.Flags().BoolVarP(&noDefaults, "no-defaults", "n", false, "only search given paths (omit cwd and ~/.ssh)")
	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "recurse into subdirectories")
	return cmd
}

func runFind(noDefaults, recursive bool, findPaths ...string) error {
	cwd, ssh := true, true
	if noDefaults {
		cwd, ssh = false, false
	}
	paths := utils.DiscoverKeyPaths(findPaths, cwd, ssh, recursive)
	out := style.NewOutput()

	fmt.Println(paths)
	if len(paths) == 0 {
		out.Error("no keys found in: " + strings.Join(findPaths, ", ")).Print()
		return nil
	}
	out.Success("* Found " + strconv.Itoa(len(paths)) + " keys")
	searchPathsDisplay := make([]string, 0, len(findPaths)+2)
	searchPathsDisplay = append(searchPathsDisplay, findPaths...)
	if ssh {
		searchPathsDisplay = append(searchPathsDisplay, "~/.ssh")
	}
	if cwd {
		cwd, _ := os.Getwd()
		searchPathsDisplay = append(searchPathsDisplay, utils.ContractHomeDirectory(cwd))
	}
	contracted := make([]string, len(searchPathsDisplay))
	for i, p := range searchPathsDisplay {
		contracted[i] = utils.ContractHomeDirectory(p)
	}
	out.Info("search paths: " + strings.Join(contracted, " "))
	out.Spacer()
	for _, path := range paths {
		out.Info(utils.ContractHomeDirectory(path))
	}
	out.Print()
	return nil
}
