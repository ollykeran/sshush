package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/platform"
	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/transport"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
	"github.com/spf13/cobra"
)

func newStartCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "start",
		Example: "sshush start\n\neval $(sshush start)\n\nsshush start --shell fish | source\n\nsshush start --shell powershell | Invoke-Expression",
		Short:   "Start the sshush agent daemon",
		Long: "Start the sshush agent daemon in the background.\n\n" +
			"When stdout is not a terminal, prints a line that sets SSH_AUTH_SOCK for the calling shell; " +
			"its syntax comes from [agent].shell in config, or --shell (" + platform.ShellNames + ").",
		Args: argsNoneOrHelp,
		RunE: runStart,
	}
	cmd.Flags().StringP("config", "c", "", "path to config file")
	addShellFlag(cmd)
	return cmd
}

// addShellFlag registers --shell, which picks the syntax of the SSH_AUTH_SOCK line.
func addShellFlag(cmd *cobra.Command) {
	cmd.Flags().String("shell", "", "syntax of the SSH_AUTH_SOCK line printed for eval: "+platform.ShellNames+" (default: [agent].shell from config, else posix)")
}

// printAuthSockLine writes the SSH_AUTH_SOCK line to stdout (for eval) only when stdout is piped.
func printAuthSockLine(authSockLine string) {
	if !isTTY(os.Stdout) {
		fmt.Fprintln(os.Stdout, authSockLine)
	}
}

func runStart(cmd *cobra.Command, _ []string) error {
	return runStartDaemon(cmd)
}

// runStartDaemon resolves config, starts the sshushd binary with SSHUSH_CONFIG, and waits for the socket.
func runStartDaemon(cmd *cobra.Command) error {
	loaded := configFrom(cmd)
	if loaded == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}

	configPath, err := runtime.ResolveConfigPath(cmd)
	if err != nil {
		return fmt.Errorf("cli: resolve config path: %w", err)
	}
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("cli: resolve absolute config path: %w", err)
	}
	cfg := *loaded
	// --shell wins over [agent].shell; reload has no --shell flag and uses config.
	shell := cfg.Shell
	if f := cmd.Flags().Lookup("shell"); f != nil && f.Changed {
		shell = f.Value.String()
	}
	absSocket, _ := transport.Abs(cfg.SocketPath)
	authSockLine, err := platform.AuthSockLine(shell, absSocket)
	if err != nil {
		return style.NewOutput().Error("--shell: unsupported shell \"" + shell + "\" (use " + platform.ShellNames + ")").AsError()
	}
	if sshushd.CheckAlreadyRunning(cfg.SocketPath) {
		printAuthSockLine(authSockLine)
		out := style.NewOutput().
			Success("* sshushd running at " + utils.DisplayPath(absSocket))
		session, err := agent.Open(cfg.SocketPath)
		if err == nil {
			defer session.Close()
			out.Spacer()
			_ = AppendKeysTo(session, out)
		}
		out.PrintErr()
		return nil
	}

	if cfg.IsExternal() {
		if cfg.SocketPath == "" {
			return style.NewOutput().
				Error("[agent].type = \"external\" but no socket found").
				Info("Set [agent].socket_path, or export SSH_AUTH_SOCK before running sshush, then try again.").
				AsError()
		}
		return style.NewOutput().
			Error("no agent reachable at " + utils.DisplayPath(cfg.SocketPath)).
			Info("[agent].type = \"external\": sshush will not start a daemon here; start your external agent (ssh-agent, 1Password, etc.) and point socket_path/SSH_AUTH_SOCK at it.").
			AsError()
	}

	out := style.NewOutput()
	loadable := 0
	for _, kp := range cfg.KeyPaths {
		if _, err := os.Stat(kp); err != nil {
			out.Warn("key not found: " + utils.DisplayPath(kp))
		} else {
			loadable++
		}
	}
	if vp := cfg.VaultPathForAgent(); vp != "" {
		resolvedVault := vault.ResolveToFile(vp)
		if _, err := os.Stat(resolvedVault); err != nil && os.IsNotExist(err) {
			if loadable > 0 {
				displayPath := utils.DisplayPath(resolvedVault)
				out.Warn("[vault].vault_path is set but vault file not found at " + displayPath + "; starting with [agent].key_paths instead")
			} else {
				displayPath := utils.DisplayPath(resolvedVault)
				return style.NewOutput().
					Error("[vault].vault_path is set but vault file not found at " + displayPath).
					Info("Run 'sshush vault init' to create it.").
					AsError()
			}
		}
	}
	if loadable == 0 && cfg.VaultPathForAgent() == "" {
		out.Error("no keys will be loaded")
	}

	pidFilePath := runtime.PidFilePath()
	if sshushd.PidFileLive(pidFilePath) {
		return style.NewOutput().
			Error("sshushd already running (pidfile " + utils.DisplayPath(pidFilePath) + " exists)").
			Info("use 'sshush reload' to apply config changes").
			AsError()
	}
	// A pidfile whose daemon is gone (it was killed, or the machine rebooted
	// with the pidfile somewhere that survives that) must not block the start.
	_ = os.Remove(pidFilePath)

	if err := sshushd.StartDaemon(absConfigPath, cfg.SocketPath); err != nil {
		return style.NewOutput().Error(err.Error()).AsError()
	}
	return startSuccess(out, &cfg, authSockLine)
}

// startSuccess prints authSockLine to stdout (for eval) only when stdout is
// piped, and the pretty success message (and any prior warnings) to stderr.
// If the agent uses a vault, prompts for passphrase and unlocks before listing keys.
func startSuccess(out *style.Output, cfg *config.Config, authSockLine string) error {
	socketPath := cfg.SocketPath
	absSocket, _ := transport.Abs(socketPath)

	printAuthSockLine(authSockLine)

	if out.Len() > 0 {
		out.Spacer()
	}
	out.Success("* sshushd started with socket: " + utils.DisplayPath(absSocket))

	session, err := agent.Open(socketPath)
	if err == nil {
		defer session.Close()
		if vp := cfg.VaultPathForAgent(); vp != "" {
			resolvedVault := vault.ResolveToFile(vp)
			store, openErr := vault.Open(resolvedVault)
			if openErr != nil {
				out.Spacer()
				out.Error("vault: " + openErr.Error())
			} else if store.GetMetadata() == nil {
				// Vault file missing or uninitialized; daemon fell back to key_paths or empty
				// Do not prompt for passphrase
			} else {
				passphrase, err := readPassphrase("Passphrase: ")
				if err != nil {
					out.Spacer()
					out.Error("unlock skipped: " + err.Error())
				} else {
					if err := session.Unlock(passphrase); err != nil {
						out.Spacer()
						out.Error("unlock failed: " + err.Error())
					}
					ClearBytes(passphrase)
				}
			}
		}
		out.Spacer()
		_ = AppendKeysTo(session, out)
	}

	out.PrintErr()
	return nil
}

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
