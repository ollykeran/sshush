package cli

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
	"github.com/spf13/cobra"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func newStartCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "start",
		Example: "sshush start\n\neval $(sshush start)",
		Short:   "Start the sshush agent daemon",
		Long:    "Start the sshush agent daemon in the background.",
		Args:    argsNoneOrHelp,
		RunE:    runStart,
	}
	cmd.Flags().StringP("config", "c", "", "path to config file")
	return cmd
}

func runStart(cmd *cobra.Command, _ []string) error {
	return runStartDaemon(cmd)
}

// runStartDaemon resolves config, starts the sshushd binary with SSHUSH_CONFIG, and waits for the socket.
func runStartDaemon(cmd *cobra.Command) error {
	if env.Config == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}

	configPath, err := runtime.ResolveConfigPath(cmd)
	if err != nil {
		return err
	}
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	cfg := *env.Config
	if sshushd.CheckAlreadyRunning(cfg.SocketPath) {
		absSocket, _ := filepath.Abs(cfg.SocketPath)
		if !isTTY(os.Stdout) {
			fmt.Fprintln(os.Stdout, "export SSH_AUTH_SOCK='"+absSocket+"'")
		}
		out := style.NewOutput().
			Success("* sshushd running at " + utils.DisplayPath(absSocket))
		conn, err := net.Dial("unix", cfg.SocketPath)
		if err == nil {
			defer conn.Close()
			client := sshagent.NewClient(conn)
			out.Spacer()
			_ = AppendKeysTo(client, out, cfg.SocketPath, cfg.VaultPathForAgent())
		}
		out.PrintErr()
		return nil
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
	if _, err := os.Stat(pidFilePath); err == nil {
		return style.NewOutput().
			Error("sshushd already running (pidfile " + utils.DisplayPath(pidFilePath) + " exists)").
			Info("use 'sshush reload' to apply config changes").
			AsError()
	}

	if err := sshushd.StartDaemon(absConfigPath, cfg.SocketPath); err != nil {
		return style.NewOutput().Error(err.Error()).AsError()
	}
	return startSuccess(out, &cfg)
}

// startSuccess prints the export line to stdout (for eval) only when stdout is
// piped, and the pretty success message (and any prior warnings) to stderr.
// If the agent uses a vault, prompts for passphrase and unlocks before listing keys.
func startSuccess(out *style.Output, cfg *config.Config) error {
	socketPath := cfg.SocketPath
	absSocket, _ := filepath.Abs(socketPath)

	if !isTTY(os.Stdout) {
		fmt.Fprintln(os.Stdout, "export SSH_AUTH_SOCK='"+absSocket+"'")
	}

	if out.Len() > 0 {
		out.Spacer()
	}
	out.Success("* sshushd started with socket: " + utils.DisplayPath(absSocket))

	conn, err := net.Dial("unix", socketPath)
	if err == nil {
		defer conn.Close()
		client := sshagent.NewClient(conn)
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
					if err := client.Unlock(passphrase); err != nil {
						out.Spacer()
						out.Error("unlock failed: " + err.Error())
					}
					ClearBytes(passphrase)
				}
			}
		}
		out.Spacer()
		_ = AppendKeysTo(client, out, socketPath, cfg.VaultPathForAgent())
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
