package cli

import (
	"os"
	"path/filepath"
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
		Long:    "Find Private OpenSSH keys in given paths, including current working directory, ~/.ssh and the config's [agent].search_dirs",
		Example: "sshush find /some/path",
		RunE: func(c *cobra.Command, args []string) error {
			var configDirs []string
			if cfg := configFrom(c); cfg != nil {
				configDirs = cfg.SearchDirs
			}
			return runFind(noDefaults, recursive, configDirs, args...)
		},
	}
	cmd.Flags().BoolVarP(&noDefaults, "no-defaults", "n", false, "only search given paths (omit cwd, ~/.ssh and [agent].search_dirs)")
	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "recurse into subdirectories")
	return cmd
}

// findSearchDirs returns the directories 'sshush find' searches besides ~/.ssh
// and the current directory: the paths given, then (unless noDefaults) the
// config's [agent].search_dirs.
func findSearchDirs(noDefaults bool, configDirs, findPaths []string) []string {
	dirs := append([]string(nil), findPaths...)
	if !noDefaults {
		dirs = append(dirs, configDirs...)
	}
	return dirs
}

// runFind lists the private keys in findPaths and, unless noDefaults is set,
// in configDirs ([agent].search_dirs), ~/.ssh and the current directory.
// Directories that are missing or cannot be read are skipped.
func runFind(noDefaults, recursive bool, configDirs []string, findPaths ...string) error {
	cwd, ssh := !noDefaults, !noDefaults
	searchDirs := findSearchDirs(noDefaults, configDirs, findPaths)
	paths := utils.DiscoverKeyPaths(searchDirs, cwd, ssh, recursive)
	out := style.NewOutput()

	searchPathsDisplay := append([]string(nil), searchDirs...)
	if ssh {
		if home, err := os.UserHomeDir(); err == nil {
			searchPathsDisplay = append(searchPathsDisplay, filepath.Join(home, ".ssh"))
		}
	}
	if cwd {
		if wd, err := os.Getwd(); err == nil {
			searchPathsDisplay = append(searchPathsDisplay, wd)
		}
	}
	displaySearch := make([]string, len(searchPathsDisplay))
	for i, p := range searchPathsDisplay {
		displaySearch[i] = utils.DisplayPath(p)
	}

	if len(paths) == 0 {
		out.Error("no keys found in: " + strings.Join(displaySearch, ", ")).Print()
		return nil
	}
	out.Success("* Found " + strconv.Itoa(len(paths)) + " keys")
	out.Info("search paths: " + strings.Join(displaySearch, " "))
	out.Spacer()
	for _, kp := range paths {
		if kp.IsSymlink {
			out.Info(utils.DisplayPath(kp.Path) + " -> " + utils.DisplayPath(kp.RealPath))
			continue
		}
		out.Info(utils.DisplayPath(kp.Path))
	}
	out.Print()
	return nil
}
