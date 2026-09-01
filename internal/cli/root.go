package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/theme"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/version"
	"github.com/spf13/cobra"
)

// env holds the merged config after file load and CLI overrides.
// Set in root PersistentPreRunE.
var env struct {
	Config *config.Config
}

var errHelpShown = errors.New("")

func isThemeCmd(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "theme" {
			return true
		}
	}
	return false
}

func isGenerateConfigCmd(cmd *cobra.Command) bool {
	return cmd != nil && cmd.Name() == "config" && cmd.Parent() != nil && cmd.Parent().Name() == "generate"
}

func suppressAgentModeIndicator(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "tui", "theme", "help", "completion",
			"validate", "create", "export", "find", "version",
			"generate", "selftest":
			return true
		case "vault":
			if c != cmd && cmd.Name() == "init" {
				return true
			}
		}
	}
	return isGenerateConfigCmd(cmd)
}

func isTTYStderr() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// commandMayStartDaemon reports commands whose PreRun runs before the agent socket may exist.
func commandMayStartDaemon(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	if cmd.Name() == "start" || cmd.Name() == "reload" {
		return true
	}
	// Bare `sshush` uses root RunE to start the daemon.
	return cmd.Root() == cmd
}

func printAgentModeIndicator(cmd *cobra.Command) {
	if env.Config == nil || !isTTYStderr() || suppressAgentModeIndicator(cmd) {
		return
	}
	configMode := env.Config.AgentBackendMode()
	sock, sockErr := getSocketPath()
	liveMode, liveOK := "", false
	// Cold start / reload-before-socket: probing here falsely looks "unreachable".
	skipProbe := sockErr != nil || sock == "" ||
		(commandMayStartDaemon(cmd) && !sshushd.CheckAlreadyRunning(sock))
	if !skipProbe {
		if session, err := agent.Open(sock); err == nil {
			if backend, err := session.Backend(); err == nil {
				liveMode, liveOK = backend.Mode, true
			}
			_ = session.Close()
		}
	}
	line := style.AgentModeIndicatorLine(configMode, liveMode, liveOK, skipProbe && sockErr == nil && sock != "")
	fmt.Fprintln(os.Stderr, line)
}

// argsNoneOrHelp rejects positional args like cobra.NoArgs, but treats
// "help" as a request to print help (matching cobra's root-level behaviour).
func argsNoneOrHelp(cmd *cobra.Command, args []string) error {
	if len(args) == 1 && args[0] == "help" {
		cmd.Help()
		cmd.SilenceUsage = true
		return errHelpShown
	}
	return cobra.NoArgs(cmd, args)
}

// resolveNoColor checks --no-color flag, NO_COLOR env var, and config theme.no_color in order;
// first truthy value wins (flag > env > config).
func resolveNoColor(cmd *cobra.Command) {
	if cmd.Flags().Changed("no-color") {
		if v, _ := cmd.Flags().GetBool("no-color"); v {
			style.SetPlainMode(true)
		}
		return
	}
	if os.Getenv("NO_COLOR") != "" {
		style.SetPlainMode(true)
		return
	}
	if env.Config != nil && env.Config.Theme.NoColor {
		style.SetPlainMode(true)
	}
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
	// External agents (real ssh-agent, 1Password, etc.) often listen on a
	// path that changes every launch (e.g. ssh-agent's /tmp/ssh-XXXXXX/agent.NNNN),
	// so [agent].socket_path is optional for type = "external" — fall back to
	// whatever SSH_AUTH_SOCK currently points at, resolved fresh on every run.
	if cfg.IsExternal() && cfg.SocketPath == "" {
		cfg.SocketPath = strings.TrimSpace(os.Getenv("SSH_AUTH_SOCK"))
	}
	return cfg, nil
}

// getSocketPath returns the agent socket path from config or SSH_AUTH_SOCK.
func getSocketPath() (string, error) {
	return runtime.ResolveSocketPath(env.Config)
}

// NewRootCommand returns the root cobra command for sshush with flags and PersistentPreRunE wired.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:          "sshush <command>",
		Short:        "SSH agent thats pretty",
		Example:      "sshush start",
		Long:         "An SSH agent and utilities with CLI and TUI. Drop in replacement for ssh-agent.",
		RunE:         func(cmd *cobra.Command, args []string) error { return runStartDaemon(cmd) },
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("version") {
				fmt.Println(version.Line("sshush"))
				os.Exit(0)
			}
			if isGenerateConfigCmd(cmd) {
				env.Config = nil
				style.SetTheme(theme.DefaultTheme())
				resolveNoColor(cmd)
				return nil
			}
			config.SetupConfig()
			configPath, err := runtime.ResolveConfigPath(cmd)
			if err != nil {
				if isThemeCmd(cmd) {
					env.Config = nil
					style.SetTheme(theme.DefaultTheme())
					resolveNoColor(cmd)
					return nil
				}
				return fmt.Errorf("cli: resolve config path: %w", err)
			}

			overrides := LoadOverrides{}
			if cmd.Flags().Changed("socket") {
				if v, err := cmd.Flags().GetString("socket"); err == nil {
					overrides.SocketPath = v
					overrides.SocketSet = true
				}
			}

			cfg, err := LoadMergedConfig(configPath, overrides)
			if err != nil {
				return fmt.Errorf("cli: load merged config: %w", err)
			}

			env.Config = &cfg
			style.SetTheme(config.ResolveThemeFromConfig(cfg))
			resolveNoColor(cmd)
			printAgentModeIndicator(cmd)
			return nil
		},
	}

	root.PersistentFlags().StringP("config", "c", "", "path to config file")
	root.PersistentFlags().StringP("socket", "s", "", "path to agent socket")
	root.PersistentFlags().Bool("no-color", false, "disable colours and fancy output (also via NO_COLOR)")
	root.Flags().BoolP("version", "v", false, "print version and exit")

	return root
}

// Execute runs the root command and handles error display; StyledError is printed via PrintErr.
func Execute() error {
	root := NewRootCommand()
	root.SilenceErrors = true // we handle all error display ourselves
	registerCommands(root)
	if err := root.Execute(); err != nil {
		if errors.Is(err, errHelpShown) {
			return nil
		}
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
