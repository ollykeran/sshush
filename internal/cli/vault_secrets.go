package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
	"github.com/ollykeran/sshush/internal/vaultops"
	"github.com/spf13/cobra"
)

func newVaultPasswdCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "passwd",
		Short: "Change the vault's passphrase",
		Long: "Change the vault's passphrase. Every key in the vault is encrypted again under the new one, and the vault is " +
			"given a new 24-word recovery phrase, since the old one no longer opens it. Prove the change with the current " +
			"passphrase, or with --use-recovery the recovery phrase. A running vault agent makes the change and stays as it " +
			"was, locked or unlocked; with none running, the vault file is changed directly.",
		Args: cobra.NoArgs,
		RunE: runVaultPasswd,
	}
	cmd.Flags().String("vault-path", "", "path to vault file (default: the running agent's vault, else [vault].vault_path)")
	cmd.Flags().Bool("use-recovery", false, "prove the change with the recovery phrase instead of the passphrase")
	cmd.Flags().Bool("no-recovery", false, "leave the vault without a recovery phrase")
	return cmd
}

func newVaultRecoveryCommand() *cobra.Command {
	recovery := &cobra.Command{
		Use:   "recovery",
		Short: "Manage the vault's recovery phrase",
	}
	regenerate := &cobra.Command{
		Use:   "regenerate",
		Short: "Give the vault a new recovery phrase",
		Long: "Give the vault a new 24-word recovery phrase, shown once. The old phrase stops working. The passphrase " +
			"is unchanged. Prove the change with the passphrase, or with --use-recovery the current recovery phrase.",
		Args: cobra.NoArgs,
		RunE: runVaultRecoveryRegenerate,
	}
	regenerate.Flags().String("vault-path", "", "path to vault file (default: the running agent's vault, else [vault].vault_path)")
	regenerate.Flags().Bool("use-recovery", false, "prove the change with the current recovery phrase instead of the passphrase")
	recovery.AddCommand(regenerate)
	return recovery
}

// readCredential asks for the passphrase or, with useRecovery, the recovery
// phrase that proves the right to change the vault.
func readCredential(useRecovery bool) (vault.Credential, error) {
	if !useRecovery {
		pass, err := readPassphrase("Current passphrase: ")
		if err != nil {
			return vault.Credential{}, style.NewOutput().Error("read passphrase: " + err.Error()).AsError()
		}
		return vault.Credential{Passphrase: pass}, nil
	}
	fmt.Fprint(os.Stderr, "Recovery phrase (24 words): ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return vault.Credential{}, style.NewOutput().Error("read recovery phrase: " + err.Error()).AsError()
	}
	return vault.Credential{Mnemonic: strings.TrimSpace(line)}, nil
}

func runVaultPasswd(cmd *cobra.Command, _ []string) error {
	e, err := vaultEnv(cmd)
	if err != nil {
		return err
	}
	useRecovery, _ := cmd.Flags().GetBool("use-recovery")
	noRecovery, _ := cmd.Flags().GetBool("no-recovery")
	cred, err := readCredential(useRecovery)
	if err != nil {
		return err
	}
	defer ClearBytes(cred.Passphrase)
	newPass, err := ReadPassphraseWithConfirm("New passphrase: ", "Confirm new passphrase: ")
	if err != nil {
		if errors.Is(err, ErrPassphrasesDoNotMatch) {
			return style.NewOutput().Error("passphrases do not match").AsError()
		}
		return style.NewOutput().Error("read passphrase: " + err.Error()).AsError()
	}
	defer ClearBytes(newPass)

	res, err := vaultops.ChangePassphrase(e, cred, newPass, !noRecovery)
	if err != nil {
		return vaultError(err)
	}
	if res.Mnemonic == "" {
		out := style.NewOutput().Success("Passphrase changed. The vault has no recovery phrase.")
		if res.RecoveryFile != "" {
			out.Info("Removed " + utils.DisplayPath(res.RecoveryFile) + ": its phrase no longer opens the vault.")
		}
		out.PrintErr()
		return nil
	}
	printRecoveryPhrase("Passphrase changed. This is the vault's new recovery phrase; store these 24 words safely:",
		res.Mnemonic, recoveryFileLine(res.RecoveryFile))
	return nil
}

func runVaultRecoveryRegenerate(cmd *cobra.Command, _ []string) error {
	e, err := vaultEnv(cmd)
	if err != nil {
		return err
	}
	useRecovery, _ := cmd.Flags().GetBool("use-recovery")
	cred, err := readCredential(useRecovery)
	if err != nil {
		return err
	}
	defer ClearBytes(cred.Passphrase)
	res, err := vaultops.RegenerateRecovery(e, cred)
	if err != nil {
		return vaultError(err)
	}
	printRecoveryPhrase("New recovery phrase. The old one no longer works; store these 24 words safely:",
		res.Mnemonic, recoveryFileLine(res.RecoveryFile))
	return nil
}

// recoveryFileLine says recovery.txt was updated, or nothing when there was
// none to update.
func recoveryFileLine(path string) string {
	if path == "" {
		return ""
	}
	return "Also written to " + utils.DisplayPath(path) + ", in place of the old phrase."
}
