package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/bridge"
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
	if cfg.RelaysToWindows() {
		return runStartRelay(&cfg, absConfigPath, authSockLine)
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

	if err := startDaemonProcess(absConfigPath, cfg.SocketPath); err != nil {
		return err
	}
	return startSuccess(out, &cfg, authSockLine)
}

// startDaemonProcess starts sshushd for the config at absConfigPath, refusing
// when the pidfile says one is already running.
func startDaemonProcess(absConfigPath, socketPath string) error {
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

	if err := sshushd.StartDaemon(absConfigPath, socketPath); err != nil {
		return style.NewOutput().Error(err.Error()).AsError()
	}
	return nil
}

// runStartRelay is start for [agent].type = "windows": the agent is the one
// sshush runs on the Windows side of this WSL distro, and the daemon here only
// relays to it. Both halves are started if they are not running, so this one
// command is all a WSL shell needs, whatever state Windows is in.
func runStartRelay(cfg *config.Config, absConfigPath, authSockLine string) error {
	absSocket, _ := transport.Abs(cfg.SocketPath)

	startedRelay := false
	if !sshushd.CheckAlreadyRunning(cfg.SocketPath) {
		if err := startDaemonProcess(absConfigPath, cfg.SocketPath); err != nil {
			return err
		}
		startedRelay = true
	}
	startedWindows, err := ensureWindowsAgent(cfg)
	if err != nil {
		return err
	}

	printAuthSockLine(authSockLine)
	out := style.NewOutput()
	state := "running at "
	if startedRelay {
		state = "started with socket: "
	}
	out.Success("* sshushd " + state + utils.DisplayPath(absSocket))
	out.Info("relaying to the sshush agent on Windows")

	session, err := agent.Open(cfg.SocketPath)
	if err == nil {
		defer session.Close()
		// What is behind the relay is the agent's to say, not this config's. A
		// locked vault is worth a prompt when this run brought something up, as
		// it is for a vault started here; a shell that finds everything already
		// running is left alone, and told the vault is locked.
		if startedRelay || startedWindows {
			if backend, berr := session.Backend(); berr == nil && backend.Mode == "vault" && backend.VaultLocked {
				promptUnlock(session, out)
			}
		}
		out.Spacer()
		_ = AppendKeysTo(session, out)
	}
	out.PrintErr()
	return nil
}

// ensureWindowsAgent makes sure an agent answers through the relay, starting
// the daemon on Windows if none does, and reports whether it had to.
func ensureWindowsAgent(cfg *config.Config) (started bool, err error) {
	if agentAnswers(cfg.SocketPath) {
		return false, nil
	}
	windows, err := bridge.Find(cfg.WindowsSSHush)
	if err != nil {
		return false, style.NewOutput().Error(err.Error()).AsError()
	}
	ctx := context.Background()
	if err := windows.StartDaemon(ctx); err != nil {
		return false, style.NewOutput().
			Error("could not start the sshush agent on Windows").
			Info(err.Error()).
			AsError()
	}
	if agentAnswers(cfg.SocketPath) {
		return true, nil
	}
	// It started, or said it did, and still nothing answers. Ask the relay
	// itself: an sshush.exe from before the relay command says so here.
	out := style.NewOutput().Error("the sshush agent on Windows is not answering through the relay")
	if said := windows.Diagnose(ctx); said != "" {
		out.Info("'" + utils.DisplayPath(windows.Path) + " relay' says: " + said)
	}
	out.Info("Both sides need the same sshush version.")
	return true, out.AsError()
}

// agentAnswers reports whether an agent completes a round trip on socketPath.
// Connecting is not enough to tell: a relay accepts the connection whether or
// not anything is behind it.
func agentAnswers(socketPath string) bool {
	session, err := agent.Open(socketPath)
	if err != nil {
		return false
	}
	defer session.Close()
	_, err = session.Backend()
	return err == nil
}

// promptUnlock asks for the vault's passphrase and unlocks the agent behind
// session, noting on out why if that does not happen.
func promptUnlock(session *agent.Session, out *style.Output) {
	passphrase, err := readPassphrase("Passphrase: ")
	if err != nil {
		out.Spacer()
		out.Error("unlock skipped: " + err.Error())
		return
	}
	defer ClearBytes(passphrase)
	if err := session.Unlock(passphrase); err != nil {
		out.Spacer()
		out.Error("unlock failed: " + err.Error())
	}
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
				promptUnlock(session, out)
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
