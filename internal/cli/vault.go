package cli

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/ollykeran/sshush/internal/agent"
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
		Short: "Vault setup and recovery",
	}
	vaultCmd.AddCommand(newVaultInitCommand())
	vaultCmd.AddCommand(newVaultListCommand())
	vaultCmd.AddCommand(newUnlockRecoveryCommand())
	return vaultCmd
}

func newVaultInitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new vault",
		Long:  "Create a new encrypted vault at the given path (or [vault].vault_path from config). Set a passphrase; optionally generate a recovery phrase.",
		Args:  cobra.NoArgs,
		RunE:  runVaultInit,
	}
	cmd.Flags().String("vault-path", "", "path to vault file (default: [vault].vault_path from config)")
	cmd.Flags().Bool("no-recovery", true, "do not generate and display a 24-word recovery phrase")
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
	withRecovery, _ := cmd.Flags().GetBool("recovery")
	if withRecovery {
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
	store, err := vault.Open(vaultPath)
	if err != nil {
		return style.NewOutput().Error("open vault: " + err.Error()).AsError()
	}
	if store.GetMetadata() == nil {
		return style.NewOutput().
			Error("vault not found or not initialized at " + utils.DisplayPath(vaultPath)).
			Info("Run 'sshush vault init' to create it.").
			AsError()
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
	if env.Config != nil && env.Config.VaultPath != "" {
		socketPath, err := getSocketPath()
		if err == nil {
			resp, err := agent.CallExtension(socketPath, vault.ExtensionVaultLocked, nil)
			if err == nil && len(resp) == 1 && resp[0] == 1 {
				passphrase, err := readPassphrase("Passphrase: ")
				if err == nil {
					conn, dialErr := net.Dial("unix", socketPath)
					if dialErr == nil {
						client := sshagent.NewClient(conn)
						_ = client.Unlock(passphrase)
						conn.Close()
					}
					clearBytes(passphrase)
				}
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
			msg = "unlock failed: wrong phrase or vault was not initialized with --recovery. Use exactly 24 words, single spaces."
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
