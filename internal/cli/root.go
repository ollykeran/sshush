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
// so the command can still run (e.g. add with --key, using SSH_AUTH_SOCK for socket).
func LoadMergedConfig(configPath string, overrides LoadOverrides) (config.Config, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		if overrides.SocketSet || overrides.KeyPathsSet {
			cfg = config.Config{KeyPaths: []string{}}
		} else {
			return config.Config{}, err
		}
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

// resolveSocketPath returns the agent socket path from config or SSH_AUTH_SOCK.
// Use for add, list, remove when connecting to a running agent.
func getSocketPath() (string, error) {
	if env.Config != nil && env.Config.SocketPath != "" {
		return env.Config.SocketPath, nil
	}
	if p := os.Getenv("SSH_AUTH_SOCK"); p != "" {
		return p, nil
	}
	return "", errors.New(style.Err("socket path required: ") + style.Pink("set SSH_AUTH_SOCK or use --socket or config"))
}

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:          "sshush",
		Short:        "SSH agent thats pretty",
		RunE:         runServe,
		SilenceUsage: true, // dont show on runtime errors
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {

			// load config from file
			configPath, _ := utils.ResolveConfigPath(cmd)
			cfg, _ := config.LoadConfig(configPath)

			// override with command line flags
			if cmd.Flags().Changed("socket") {
				cfg.SocketPath, _ = cmd.Flags().GetString("socket")
			}
			if cmd.Flags().Changed("key") {
				cfg.KeyPaths, _ = cmd.Flags().GetStringArray("key")
			}

			// set for global use
			env.Config = &cfg
			return nil
		},
	}

	// root.PersistentFlags().String("config", "", "config file path (default: $SSHUSH_CONFIG or ~/.config/sshush/config.toml)")
	// root.PersistentFlags().StringVar(&socketPath, "socket", "", "override socket path from config")
	// root.PersistentFlags().StringArrayVar(&keyPaths, "key", nil, "key file path to add (can be repeated)")

	return root
}

func Execute() error {
	root := NewRootCommand()
	registerCommands(root)
	return root.Execute()
}
