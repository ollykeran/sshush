package cli

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/openssh"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
	"github.com/spf13/cobra"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
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

// openInitializedVaultStore opens the vault from --vault-path or config and checks it is initialized.
func openInitializedVaultStore(cmd *cobra.Command) (*vault.VaultStore, string, error) {
	var vaultPath string
	if cmd.Flags().Changed("vault-path") {
		vaultPath, _ = cmd.Flags().GetString("vault-path")
	} else if env.Config != nil && env.Config.VaultPath != "" {
		vaultPath = env.Config.VaultPath
	}
	if vaultPath == "" {
		return nil, "", style.NewOutput().Error("vault path required: set [vault].vault_path in config or use --vault-path").AsError()
	}
	vaultPath = utils.ExpandHomeDirectory(vaultPath)
	vaultPath = vault.ResolveToFile(vaultPath)
	store, err := vault.Open(vaultPath)
	if err != nil {
		return nil, vaultPath, style.NewOutput().Error("open vault: " + err.Error()).AsError()
	}
	if store.GetMetadata() == nil {
		return nil, vaultPath, style.NewOutput().
			Error("vault not found or not initialized at " + utils.DisplayPath(vaultPath)).
			Info("Run 'sshush vault init' to create it.").
			AsError()
	}
	return store, vaultPath, nil
}

// unlockVaultAgentIfLocked prompts for passphrase and unlocks the agent when it uses the same vault file.
func unlockVaultAgentIfLocked(socketPath, resolvedVaultPath string) {
	if env.Config == nil {
		return
	}
	agentVaultFile := ""
	if env.Config.AgentVault && env.Config.VaultPath != "" {
		agentVaultFile = vault.ResolveToFile(utils.ExpandHomeDirectory(env.Config.VaultPath))
	}
	if agentVaultFile == "" || resolvedVaultPath != agentVaultFile {
		return
	}
	resp, err := agent.CallExtension(socketPath, vault.ExtensionVaultLocked, nil)
	if err != nil || len(resp) != 1 || resp[0] != 1 {
		return
	}
	passphrase, err := readPassphrase("Passphrase: ")
	if err != nil {
		return
	}
	conn, dialErr := net.Dial("unix", socketPath)
	if dialErr == nil {
		client := sshagent.NewClient(conn)
		_ = client.Unlock(passphrase)
		conn.Close()
	}
	clearBytes(passphrase)
}

func resolveVaultSelectorArg(store *vault.VaultStore, arg string) (vault.Identity, error) {
	path := utils.ExpandHomeDirectory(arg)
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		pubKey, _, _, err := agent.ParseKeyFromPath(path)
		if err == nil {
			return vault.ResolveIdentityByFingerprint(store, ssh.FingerprintSHA256(pubKey))
		}
		if errors.Is(err, openssh.ErrEncryptedPrivateKey) {
			return vault.Identity{}, err
		}
	}
	return vault.ResolveIdentity(store, arg)
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
	var vaultPath string
	if cmd.Flags().Changed("vault-path") {
		vaultPath, _ = cmd.Flags().GetString("vault-path")
	} else if env.Config != nil && env.Config.VaultPath != "" {
		vaultPath = env.Config.VaultPath
	}
	if vaultPath == "" {
		return style.NewOutput().Error("vault path required: set [vault].vault_path in config or use --vault-path").AsError()
	}
	vaultPath = utils.ExpandHomeDirectory(vaultPath)
	vaultPath = vault.ResolveToFile(vaultPath)
	if fi, err := os.Stat(vaultPath); err == nil && !fi.IsDir() {
		return style.NewOutput().Error("vault already exists at " + utils.DisplayPath(vaultPath)).AsError()
	}
	passphrase, err := readPassphrase("Passphrase: ")
	if err != nil {
		return style.NewOutput().Error("read passphrase: " + err.Error()).AsError()
	}
	defer func() { clearBytes(passphrase) }()
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr)
	confirm, err := readPassphrase("Confirm passphrase: ")
	if err != nil {
		return style.NewOutput().Error("read confirmation: " + err.Error()).AsError()
	}
	defer func() { clearBytes(confirm) }()
	if string(passphrase) != string(confirm) {
		return style.NewOutput().Error("passphrases do not match").AsError()
	}
	store, err := vault.Open(vaultPath)
	if err != nil {
		return style.NewOutput().Error("create vault: " + err.Error()).AsError()
	}
	if err := vault.Init(store, passphrase); err != nil {
		return style.NewOutput().Error("init vault: " + err.Error()).AsError()
	}
	noRecovery, _ := cmd.Flags().GetBool("no-recovery")
	if !noRecovery {
		mnemonic, err := vault.GenerateRecoveryMnemonic()
		if err != nil {
			return style.NewOutput().Error("generate recovery phrase: " + err.Error()).AsError()
		}
		if err := vault.EnableRecoveryWithPassphrase(store, passphrase, mnemonic); err != nil {
			return style.NewOutput().Error("enable recovery: " + err.Error()).AsError()
		}
		// Write recovery.txt in same dir as vault for easy copy
		recoveryTxt := filepath.Join(filepath.Dir(vaultPath), "recovery.txt")
		if err := os.WriteFile(recoveryTxt, []byte(mnemonic+"\n"), 0600); err != nil {
			return style.NewOutput().Error("write recovery.txt: " + err.Error()).AsError()
		}
		if recoveryFile, _ := cmd.Flags().GetString("recovery-file"); recoveryFile != "" {
			recoveryFile = utils.ExpandHomeDirectory(recoveryFile)
			if err := os.WriteFile(recoveryFile, []byte(mnemonic+"\n"), 0600); err != nil {
				return style.NewOutput().Error("write recovery file: " + err.Error()).AsError()
			}
		}

		// Print to terminal with spacers so user doesn't copy the wrong thing
		fmt.Fprintln(os.Stderr)
		out := style.NewOutput().
			Success("Vault initialized with recovery phrase. Store these 24 words safely:").
			Spacer()
		for _, line := range wordWrap(mnemonic, 60) {
			out.Info(line)
		}
		out.Spacer().
			Info("Store this phrase offline; it is not saved anywhere else.")
		out.Success("Also written to " + utils.DisplayPath(recoveryTxt) + " (mode 0600)")
		if err := CopyToClipboard(mnemonic); err == nil {
			out.Success("Copied to clipboard.")
		}
		fmt.Fprintln(os.Stderr, style.BoxWithMaxWidth(out.String(), 72))
		os.Stderr.Sync()
	} else {
		style.NewOutput().Success("Vault initialized at " + utils.DisplayPath(vaultPath)).PrintErr()
	}
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
	store, vaultPath, err := openInitializedVaultStore(cmd)
	if err != nil {
		return err
	}
	identities, err := vault.ListIdentities(store)
	if err != nil {
		return style.NewOutput().Error("list identities: " + err.Error()).AsError()
	}
	if len(identities) == 0 {
		style.NewOutput().Warn("no keys in vault").PrintTo(os.Stdout)
		return nil
	}

	var loadedSet map[string]struct{}
	haveAgent := false
	socketPath, sockErr := getSocketPath()
	if sockErr == nil {
		agentVaultFile := ""
		if env.Config != nil && env.Config.AgentVault && env.Config.VaultPath != "" {
			agentVaultFile = vault.ResolveToFile(utils.ExpandHomeDirectory(env.Config.VaultPath))
		}
		if agentVaultFile != "" && vaultPath == agentVaultFile {
			unlockVaultAgentIfLocked(socketPath, vaultPath)
		}
		keys, err := agent.ListKeysFromSocket(socketPath)
		if err == nil {
			loadedSet = make(map[string]struct{})
			for _, k := range keys {
				if pub, err := ssh.ParsePublicKey(k.Blob); err == nil {
					loadedSet[ssh.FingerprintSHA256(pub)] = struct{}{}
				}
			}
			haveAgent = true
		}
	}

	out := style.NewOutput()
	out.Add(style.Highlight(fmt.Sprintf("%-70s  %-6s  %-8s  %-20s  %s", "FINGERPRINT", "LOADED", "AUTOLOAD", "COMMENT", "TYPE")))
	maxTypeLen := 0
	for _, id := range identities {
		if len(id.KeyType) > maxTypeLen {
			maxTypeLen = len(id.KeyType)
		}
	}
	for _, id := range identities {
		loaded := "n/a"
		if haveAgent {
			if _, ok := loadedSet[id.Fingerprint]; ok {
				loaded = "yes"
			} else {
				loaded = "no"
			}
		}
		autoload := "no"
		if id.Autoload {
			autoload = "yes"
		}
		comment := id.Comment
		if len(comment) > 20 {
			comment = comment[:17] + "..."
		}
		out.Add(style.Highlight(fmt.Sprintf("%-70s  %-6s  %-8s  %-20s  %-*s", id.Fingerprint, loaded, autoload, comment, maxTypeLen, id.KeyType)))
	}
	out.PrintTo(os.Stdout)
	return nil
}

func newVaultAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "add <key_paths...>",
		Example: "sshush vault add ~/.ssh/id_ed25519",
		Short:   "Add private key file(s) to the vault via the running agent",
		Long: "Add unencrypted OpenSSH private key(s) to the vault-backed agent. Requires [agent].vault = true and sshush start. " +
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
	socketPath, err := getSocketPath()
	if err != nil {
		return style.NewOutput().Error("failed to get socket path").AsError()
	}
	if !sshushd.CheckAlreadyRunning(socketPath) {
		return style.NewOutput().Error("Agent not running. Please start the agent with 'sshush start'").AsError()
	}
	mode, live := agent.LiveBackendMode(socketPath)
	if !live || mode != "vault" {
		return style.NewOutput().Error("vault add requires a running vault agent; set [agent].vault = true and run 'sshush start'").AsError()
	}
	noAutoload, _ := cmd.Flags().GetBool("no-autoload")
	autoload := !noAutoload

	before, err := agent.ListKeysFromSocket(socketPath)
	if err != nil {
		return style.NewOutput().Error("failed to list keys from socket").AsError()
	}
	out := style.NewOutput()
	for _, arg := range args {
		path := utils.ExpandHomeDirectory(arg)
		if _, err := os.Stat(path); err != nil {
			resolved, resolveErr := resolveKeyPathByComment(arg, env.Config)
			if resolveErr != nil {
				return resolveErr
			}
			path = utils.ExpandHomeDirectory(resolved)
		}
		if err := vault.AddPrivateKeyFileToSocket(socketPath, path, autoload); err != nil {
			msg := err.Error()
			if msg == "agent: generic extension failure" && env.Config.AgentVault && env.Config.VaultPath != "" {
				msg = "vault is locked; unlock first with 'sshush unlock' or 'sshush vault unlock-recovery'"
			} else {
				msg = "failed to add key: " + msg
			}
			return style.NewOutput().Error(msg).AsError()
		}
	}
	out.PrintErr()
	after, _ := agent.ListKeysFromSocket(socketPath)
	printKeysDiff(agentKeysToEntries(before), agentKeysToEntries(after)).Print()
	return nil
}

func newVaultRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove <fingerprint|comment|key_path...>",
		Short:   "Remove identity(ies) from the vault store",
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
	store, vaultPath, err := openInitializedVaultStore(cmd)
	if err != nil {
		return err
	}
	socketPath, err := getSocketPath()
	if err != nil {
		return err
	}
	if !sshushd.CheckAlreadyRunning(socketPath) {
		return style.NewOutput().Error("Agent not running. Please start the agent with 'sshush start'").AsError()
	}
	mode, live := agent.LiveBackendMode(socketPath)
	if !live || mode != "vault" {
		return style.NewOutput().Error("vault remove requires a running vault agent").AsError()
	}
	unlockVaultAgentIfLocked(socketPath, vaultPath)

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return style.NewOutput().Error("cannot connect to agent: " + err.Error()).AsError()
	}
	defer conn.Close()
	client := sshagent.NewClient(conn)

	before, err := agent.ListKeysFromSocket(socketPath)
	if err != nil {
		before = nil
	}

	for _, arg := range args {
		id, err := resolveVaultSelectorArg(store, arg)
		if err != nil {
			if errors.Is(err, vault.ErrAmbiguousComment) {
				return style.NewOutput().Error("ambiguous comment: multiple vault identities share that comment; use fingerprint").AsError()
			}
			if errors.Is(err, vault.ErrIdentityNotFound) {
				return style.NewOutput().Error("no vault identity matches " + arg).AsError()
			}
			if errors.Is(err, openssh.ErrEncryptedPrivateKey) {
				return style.NewOutput().Error(err.Error()).AsError()
			}
			return style.NewOutput().Error(err.Error()).AsError()
		}
		pubKey, err := ssh.ParsePublicKey(id.PublicKey)
		if err != nil {
			return style.NewOutput().Error("parse stored public key: " + err.Error()).AsError()
		}
		if err := client.Remove(pubKey); err != nil {
			return style.NewOutput().Error(fmt.Sprintf("remove %s: %v", arg, err)).AsError()
		}
	}

	after, _ := agent.ListKeysFromSocket(socketPath)
	printKeysDiff(agentKeysToEntries(before), agentKeysToEntries(after)).Print()
	return nil
}

func newVaultLoadCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "load <fingerprint|comment|key_path...>",
		Short:   "Load non-autoload vault key(s) into this agent session",
		Long: "For identities stored with autoload off, mark them visible in the running agent until it restarts, " +
			"so ssh can use them without the PEM file. Requires an unlocked vault agent.",
		RunE:    runVaultLoad,
		Args:    cobra.MinimumNArgs(1),
	}
	cmd.Flags().String("vault-path", "", "path to vault file (default: [vault].vault_path from config)")
	return cmd
}

func runVaultLoad(cmd *cobra.Command, args []string) error {
	if env.Config == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}
	store, vaultPath, err := openInitializedVaultStore(cmd)
	if err != nil {
		return err
	}
	socketPath, err := getSocketPath()
	if err != nil {
		return err
	}
	if !sshushd.CheckAlreadyRunning(socketPath) {
		return style.NewOutput().Error("Agent not running. Please start the agent with 'sshush start'").AsError()
	}
	mode, live := agent.LiveBackendMode(socketPath)
	if !live || mode != "vault" {
		return style.NewOutput().Error("vault load requires a running vault agent").AsError()
	}
	unlockVaultAgentIfLocked(socketPath, vaultPath)

	for _, arg := range args {
		id, err := resolveVaultSelectorArg(store, arg)
		if err != nil {
			if errors.Is(err, vault.ErrAmbiguousComment) {
				return style.NewOutput().Error("ambiguous comment: multiple vault identities share that comment; use fingerprint").AsError()
			}
			if errors.Is(err, vault.ErrIdentityNotFound) {
				return style.NewOutput().Error("no vault identity matches " + arg).AsError()
			}
			if errors.Is(err, openssh.ErrEncryptedPrivateKey) {
				return style.NewOutput().Error(err.Error()).AsError()
			}
			return style.NewOutput().Error(err.Error()).AsError()
		}
		_, err = agent.CallExtension(socketPath, vault.ExtensionVaultSessionLoad, []byte(id.Fingerprint))
		if err != nil {
			msg := err.Error()
			if msg == "agent: generic extension failure" {
				msg = "vault load failed (wrong fingerprint, vault locked, or key already autoloads)"
			}
			return style.NewOutput().Error(msg).AsError()
		}
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
	selectors := args[1:]

	store, vaultPath, err := openInitializedVaultStore(cmd)
	if err != nil {
		return err
	}
	socketPath, err := getSocketPath()
	if err != nil {
		return err
	}
	if !sshushd.CheckAlreadyRunning(socketPath) {
		return style.NewOutput().Error("Agent not running. Please start the agent with 'sshush start'").AsError()
	}
	mode, live := agent.LiveBackendMode(socketPath)
	if !live || mode != "vault" {
		return style.NewOutput().Error("vault autoload requires a running vault agent").AsError()
	}
	unlockVaultAgentIfLocked(socketPath, vaultPath)

	for _, arg := range selectors {
		id, err := resolveVaultSelectorArg(store, arg)
		if err != nil {
			if errors.Is(err, vault.ErrAmbiguousComment) {
				return style.NewOutput().Error("ambiguous comment: multiple vault identities share that comment; use fingerprint").AsError()
			}
			if errors.Is(err, vault.ErrIdentityNotFound) {
				return style.NewOutput().Error("no vault identity matches " + arg).AsError()
			}
			if errors.Is(err, openssh.ErrEncryptedPrivateKey) {
				return style.NewOutput().Error(err.Error()).AsError()
			}
			return style.NewOutput().Error(err.Error()).AsError()
		}
		payload := vault.BuildSetAutoloadPayload(id.Fingerprint, on)
		_, err = agent.CallExtension(socketPath, vault.ExtensionVaultSetAutoload, payload)
		if err != nil {
			msg := err.Error()
			if msg == "agent: generic extension failure" {
				msg = "vault autoload failed (vault locked or identity not found)"
			}
			return style.NewOutput().Error(msg).AsError()
		}
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
	conn, err := net.Dial("unix", env.Config.SocketPath)
	if err != nil {
		return style.NewOutput().Error("cannot connect to agent: " + err.Error()).AsError()
	}
	defer conn.Close()
	client := sshagent.NewClient(conn)
	fmt.Fprint(os.Stderr, "Recovery phrase (24 words): ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return style.NewOutput().Error("read recovery phrase: " + err.Error()).AsError()
	}
	mnemonic := strings.TrimSpace(line)
	// Extension type and payload: we use "unlock-recovery" with mnemonic as contents
	resp, err := client.Extension("unlock-recovery", []byte(mnemonic))
	if err != nil {
		msg := err.Error()
		if msg == "agent: generic extension failure" {
			msg = "unlock failed: wrong phrase or vault was created with --no-recovery. Use exactly 24 words, single spaces."
		} else {
			msg = "unlock with recovery failed: " + msg
		}
		return style.NewOutput().Error(msg).AsError()
	}
	_ = resp
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

func clearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
