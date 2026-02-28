package cli

import (
	"errors"
	"os"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
)

// env holds the merged config after file load and CLI overrides.
// Set in root PersistentPreRunE.
var env struct {
	Config *config.Config
}

// LoadOverrides holds CLI flag values and whether each was set (so we only override when set).
type LoadOverrides struct {
	SocketPath  string
	KeyPaths    []string
	SocketSet   bool
	KeyPathsSet bool
}

// LoadMergedConfig loads config from path and applies overrides. If the config file
// is missing but the user supplied socket or key overrides, an empty config is used
// so the command can still run (e.g. using SSH_AUTH_SOCK for socket).
func LoadMergedConfig(configPath string, overrides LoadOverrides) (config.Config, error) {
	if _, statErr := os.Stat(configPath); statErr != nil {
		if overrides.SocketSet || overrides.KeyPathsSet {
			cfg := config.Config{KeyPaths: []string{}}
			if overrides.SocketSet {
				cfg.SocketPath = overrides.SocketPath
			}
			if overrides.KeyPathsSet {
				for _, p := range overrides.KeyPaths {
					cfg.KeyPaths = append(cfg.KeyPaths, utils.ExpandHomeDirectory(p))
				}
			}
			return cfg, nil
		}
		return config.Config{}, statErr
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return config.Config{}, err
	}
	if overrides.SocketSet {
		cfg.SocketPath = overrides.SocketPath
	}
	if overrides.KeyPathsSet {
		for _, p := range overrides.KeyPaths {
			cfg.KeyPaths = append(cfg.KeyPaths, utils.ExpandHomeDirectory(p))
		}
	}
	return cfg, nil
}

// getSocketPath returns the agent socket path from config or SSH_AUTH_SOCK.
func getSocketPath() (string, error) {
	if env.Config != nil && env.Config.SocketPath != "" {
		return env.Config.SocketPath, nil
	}
	if p := os.Getenv("SSH_AUTH_SOCK"); p != "" {
		return p, nil
	}
	return "", style.NewOutput().
		Error("socket path required").
		Info("set SSH_AUTH_SOCK or use --socket or config").
		AsError()
}

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:          "sshush",
		Short:        "SSH agent thats pretty",
		RunE:         func(cmd *cobra.Command, args []string) error { return runStartDaemon(cmd) },
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			configPath, _ := utils.ResolveConfigPath(cmd)
			cfg, _ := config.LoadConfig(configPath)

			if cmd.Flags().Changed("socket") {
				cfg.SocketPath, _ = cmd.Flags().GetString("socket")
			}

			env.Config = &cfg
			return nil
		},
	}
	return root
}

func Execute() error {
	root := NewRootCommand()
	root.SilenceErrors = true // we handle all error display ourselves
	registerCommands(root)
	if err := root.Execute(); err != nil {
		var se *style.StyledError
		if errors.As(err, &se) {
			se.PrintErr()
		} else {
			style.NewOutput().Error(err.Error()).PrintErr()
		}
		return err
	}
	return nil
}
