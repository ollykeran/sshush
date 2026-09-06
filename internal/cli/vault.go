package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vaultops"
	"github.com/spf13/cobra"
)

func newVaultCommand() *cobra.Command {
	vaultCmd := &cobra.Command{
		Use:   "vault",
		Short: "Vault setup, identity management, and recovery",
	}
	vaultCmd.AddCommand(newVaultInitCommand())
	vaultCmd.AddCommand(newVaultListCommand())
	vaultCmd.AddCommand(newVaultAddCommand())
	vaultCmd.AddCommand(newVaultRemoveCommand())
	vaultCmd.AddCommand(newVaultLoadCommand())
	vaultCmd.AddCommand(newVaultAutoloadCommand())
	vaultCmd.AddCommand(newUnlockRecoveryCommand())
	return vaultCmd
}

// vaultEnv builds the vaultops.Env for a vault subcommand from --vault-path,
// config and the resolved socket. AskPassphrase is set, so a locked agent
// serving this very vault is unlocked in passing rather than refused.
func vaultEnv(cmd *cobra.Command) (vaultops.Env, error) {
	var vaultPath string
	if cmd.Flags().Changed("vault-path") {
		vaultPath, _ = cmd.Flags().GetString("vault-path")
	} else if env.Config != nil {
		vaultPath = env.Config.VaultPath
	}
	socketPath, err := getSocketPath()
	if err != nil {
		return vaultops.Env{}, style.NewOutput().Error("failed to get socket path").AsError()
	}
	agentVaultPath := ""
	if env.Config != nil && env.Config.IsVault() {
		agentVaultPath = env.Config.VaultPath
	}
	return vaultops.Env{
		VaultPath:      vaultPath,
		SocketPath:     socketPath,
		AgentVaultPath: agentVaultPath,
		AskPassphrase:  readPassphrase,
	}, nil
}

// vaultError renders a vaultops failure as sshush's standard styled error: the
// sentence, then the remedy on its own line.
func vaultError(err error) error {
	out := style.NewOutput().Error(err.Error())
	if hint := vaultops.HintOf(err); hint != "" {
		out.Info(hint)
	}
	return out.AsError()
}

func newVaultInitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new vault",
		Long:  "Create a new encrypted vault at the given path (or [vault].vault_path from config). Set a passphrase; by default a 24-word recovery phrase is generated, shown, and written beside the vault as recovery.txt. Use --no-recovery to skip.",
		Args:  cobra.NoArgs,
		RunE:  runVaultInit,
	}
	cmd.Flags().String("vault-path", "", "path to vault file (default: [vault].vault_path from config)")
	cmd.Flags().Bool("no-recovery", false, "do not generate and display a 24-word recovery phrase")
	cmd.Flags().String("recovery-file", "", "also write the recovery phrase to this file (mode 0600)")
	return cmd
}

func runVaultInit(cmd *cobra.Command, _ []string) error {
	e, err := vaultEnv(cmd)
	if err != nil {
		return err
	}
	// Fail on an existing vault before asking for a passphrase, so a mistyped
	// --vault-path costs one message rather than two prompts.
	if _, err := vaultops.InitTarget(e); err != nil {
		return vaultError(err)
	}
	passphrase, err := ReadPassphraseWithConfirm("Passphrase: ", "Confirm passphrase: ")
	if err != nil {
		if errors.Is(err, ErrPassphrasesDoNotMatch) {
			return style.NewOutput().Error("passphrases do not match").AsError()
		}
		return style.NewOutput().Error("read passphrase: " + err.Error()).AsError()
	}
	defer ClearBytes(passphrase)

	noRecovery, _ := cmd.Flags().GetBool("no-recovery")
	recoveryFile, _ := cmd.Flags().GetString("recovery-file")
	res, err := vaultops.Init(e, passphrase, vaultops.InitOptions{
		Recovery:     !noRecovery,
		RecoveryFile: recoveryFile,
	})
	if err != nil {
		return vaultError(err)
	}
	if res.Mnemonic == "" {
		style.NewOutput().Success("Vault initialized at " + utils.DisplayPath(res.VaultPath)).PrintErr()
		return nil
	}

	// Print to terminal with spacers so user doesn't copy the wrong thing
	fmt.Fprintln(os.Stderr)
	out := style.NewOutput().
		Success("Vault initialized with recovery phrase. Store these 24 words safely:").
		Spacer()
	for _, line := range wordWrap(res.Mnemonic, 60) {
		out.Info(line)
	}
	out.Spacer().
		Info("Store this phrase offline; it is not saved anywhere else.")
	out.Success("Also written to " + utils.DisplayPath(res.RecoveryFile) + " (mode 0600)")
	if err := CopyToClipboard(res.Mnemonic); err == nil {
		out.Success("Copied to clipboard.")
	}
	fmt.Fprintln(os.Stderr, style.BoxWithMaxWidth(out.String(), 72))
	os.Stderr.Sync()
	return nil
}

func newVaultListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all keys in the vault",
		Long:  "Show fingerprint, loaded (in current agent), autoload, comment, and key type for each identity. If the vault is locked, prompts for passphrase to unlock.",
		Args:  cobra.NoArgs,
		RunE:  runVaultList,
	}
	cmd.Flags().String("vault-path", "", "path to vault file (default: [vault].vault_path from config)")
	return cmd
}

func runVaultList(cmd *cobra.Command, _ []string) error {
	e, err := vaultEnv(cmd)
	if err != nil {
		return err
	}
	res, err := vaultops.List(e)
	if err != nil {
		return vaultError(err)
	}
	if !res.Initialized {
		return style.NewOutput().
			Error("vault not found or not initialized at " + utils.DisplayPath(res.VaultPath)).
			Info("Run 'sshush vault init' to create it.").
			AsError()
	}
	if len(res.Identities) == 0 {
		style.NewOutput().Warn("no keys in vault").PrintTo(os.Stdout)
		return nil
	}

	out := style.NewOutput()
	out.Add(style.Highlight(fmt.Sprintf("%-70s  %-6s  %-8s  %-20s  %-10s  %s", "FINGERPRINT", "LOADED", "AUTOLOAD", "COMMENT", "TYPE", "FILEPATH")))
	maxTypeLen := 0
	for _, id := range res.Identities {
		if len(id.KeyType) > maxTypeLen {
			maxTypeLen = len(id.KeyType)
		}
	}
	for _, id := range res.Identities {
		autoload := "no"
		if id.Autoload {
			autoload = "yes"
		}
		comment := id.Comment
		if len(comment) > 20 {
			comment = comment[:17] + "..."
		}
		keyFile := id.Filepath
		if keyFile == "" {
			keyFile = "-"
		}
		out.Add(style.Highlight(fmt.Sprintf("%-70s  %-6s  %-8s  %-20s  %-*s  %s", id.Fingerprint, id.Loaded, autoload, comment, maxTypeLen, id.KeyType, keyFile)))
	}
	out.PrintTo(os.Stdout)
	return nil
}

func newVaultAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "add <key_paths...>",
		Example: "sshush vault add ~/.ssh/id_ed25519",
		Short:   "Add private key file(s) to the vault via the running agent",
		Long: "Add unencrypted OpenSSH private key(s) to the vault-backed agent. Requires [agent].type = \"vault\" and sshush start. " +
			"Keys are stored encrypted with autoload on by default; use --no-autoload for session-only until daemon restart.",
		RunE: runVaultAdd,
	}
	cmd.Flags().Bool("no-autoload", false, "store key without autoload (session-only until daemon restart)")
	return cmd
}

func runVaultAdd(cmd *cobra.Command, args []string) error {
	if env.Config == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}
	if len(args) == 0 {
		_ = cmd.Usage()
		return style.NewOutput().Error("at least one key path is required").AsError()
	}
	e, err := vaultEnv(cmd)
	if err != nil {
		return err
	}
	// Resolve every argument to a real file before touching the agent, so an
	// unrecognised one costs nothing rather than half a command.
	paths := make([]string, 0, len(args))
	for _, arg := range args {
		path := utils.ExpandHomeDirectory(arg)
		if _, statErr := os.Stat(path); statErr != nil {
			resolved, resolveErr := resolveKeyPathByComment(arg, env.Config)
			if resolveErr != nil {
				return resolveErr
			}
			path = utils.ExpandHomeDirectory(resolved)
		}
		paths = append(paths, path)
	}
	noAutoload, _ := cmd.Flags().GetBool("no-autoload")

	res, err := vaultops.Add(e, paths, !noAutoload)
	if err != nil {
		return vaultError(err)
	}
	printKeysDiff(agentKeysToEntries(res.Before), agentKeysToEntries(res.After)).Print()
	return nil
}

func newVaultRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <fingerprint|comment|key_path...>",
		Short: "Remove identity(ies) from the vault store",
		Long: "Remove keys from the encrypted vault by SHA256 fingerprint, comment, or private key file path. " +
			"Works even when the key is not listed by the agent (for example after restart with autoload off). " +
			"Requires a running vault agent and an unlocked vault.",
		RunE: runVaultRemove,
	}
	cmd.Flags().String("vault-path", "", "path to vault file (default: [vault].vault_path from config)")
	return cmd
}

func runVaultRemove(cmd *cobra.Command, args []string) error {
	if env.Config == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}
	if len(args) == 0 {
		_ = cmd.Usage()
		return style.NewOutput().Error("at least one selector is required").AsError()
	}
	e, err := vaultEnv(cmd)
	if err != nil {
		return err
	}
	res, err := vaultops.Remove(e, args)
	if err != nil {
		return vaultError(err)
	}
	printKeysDiff(agentKeysToEntries(res.Before), agentKeysToEntries(res.After)).Print()
	return nil
}

func newVaultLoadCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "load <fingerprint|comment|key_path...>",
		Short: "Load non-autoload vault key(s) into this agent session",
		Long: "For identities stored with autoload off, mark them visible in the running agent until it restarts, " +
			"so ssh can use them without the PEM file. Requires an unlocked vault agent.",
		RunE: runVaultLoad,
		Args: cobra.MinimumNArgs(1),
	}
	cmd.Flags().String("vault-path", "", "path to vault file (default: [vault].vault_path from config)")
	return cmd
}

func runVaultLoad(cmd *cobra.Command, args []string) error {
	if env.Config == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}
	e, err := vaultEnv(cmd)
	if err != nil {
		return err
	}
	if _, err := vaultops.SessionLoad(e, args); err != nil {
		return vaultError(err)
	}
	style.NewOutput().Success("Loaded into agent session.").PrintErr()
	return nil
}

func parseAutoloadOnOff(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "yes", "true", "1":
		return true, nil
	case "off", "no", "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("first argument must be on or off")
	}
}

func newVaultAutoloadCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "autoload (on|off) <fingerprint|comment|key_path...>",
		Example: "sshush vault autoload on SHA256:abcd...\nsshush vault autoload off my-key-comment",
		Short:   "Set persistent autoload on or off for vault identity(ies)",
		Long: "Update whether each identity loads automatically after daemon restart. " +
			"Requires an unlocked vault agent.",
		RunE: runVaultAutoload,
		Args: cobra.MinimumNArgs(2),
	}
	cmd.Flags().String("vault-path", "", "path to vault file (default: [vault].vault_path from config)")
	return cmd
}

func runVaultAutoload(cmd *cobra.Command, args []string) error {
	if env.Config == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}
	on, err := parseAutoloadOnOff(args[0])
	if err != nil {
		return style.NewOutput().Error(err.Error()).AsError()
	}
	e, err := vaultEnv(cmd)
	if err != nil {
		return err
	}
	if _, err := vaultops.SetAutoload(e, args[1:], on); err != nil {
		return vaultError(err)
	}
	style.NewOutput().Success("Autoload updated.").PrintErr()
	return nil
}

func newUnlockRecoveryCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "unlock-recovery",
		Short: "Unlock the vault using the recovery phrase",
		Long:  "Connect to the running agent and unlock the vault using the 24-word recovery phrase.",
		Args:  cobra.NoArgs,
		RunE:  runUnlockRecovery,
	}
}

func runUnlockRecovery(cmd *cobra.Command, _ []string) error {
	if env.Config == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}
	e, err := vaultEnv(cmd)
	if err != nil {
		return err
	}
	// Check the agent before asking for 24 words, not after.
	if err := vaultops.RequireVaultAgent(e, "unlock-recovery"); err != nil {
		return vaultError(err)
	}
	fmt.Fprint(os.Stderr, "Recovery phrase (24 words): ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return style.NewOutput().Error("read recovery phrase: " + err.Error()).AsError()
	}
	if err := vaultops.UnlockRecovery(e, strings.TrimSpace(line)); err != nil {
		return vaultError(err)
	}
	style.NewOutput().Success("Vault unlocked with recovery phrase.").PrintErr()
	return nil
}

func wordWrap(s string, maxLineLen int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	var line string
	for _, w := range words {
		if line == "" {
			line = w
			continue
		}
		if len(line)+1+len(w) <= maxLineLen {
			line += " " + w
		} else {
			lines = append(lines, line)
			line = w
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
