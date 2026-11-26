package vaultops

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/ollykeran/sshush/internal/secfile"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
)

// A vault is one file, so moving it between PCs is copying it. Backup and
// Import are that copy, with the checks that make it safe: that the file is a
// vault, that the passphrase opens it, and that nothing is lost on the way.

// FileSummary is what a vault file says about itself, read without unlocking it.
type FileSummary struct {
	Keys     int
	Recovery bool
	Created  time.Time
}

// readVaultFile reads path and checks it is an initialized vault, returning
// its bytes exactly as read so that what is copied is what was checked.
func readVaultFile(path string) ([]byte, FileSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, FileSummary{}, &OpError{Code: CodeNotAVault, Msg: "no file at " + utils.DisplayPath(path), Err: err}
		}
		return nil, FileSummary{}, &OpError{Code: CodeLocalIO, Msg: "read " + utils.DisplayPath(path) + ": " + err.Error(), Err: err}
	}
	var f vault.VaultFile
	if err := json.Unmarshal(data, &f); err != nil || f.Metadata == nil || len(f.Metadata.Salt) == 0 || len(f.Metadata.Canary) == 0 {
		return nil, FileSummary{}, &OpError{
			Code: CodeNotAVault,
			Msg:  utils.DisplayPath(path) + " is not an sshush vault",
			Err:  err,
		}
	}
	sum := FileSummary{Keys: len(f.Identities), Recovery: len(f.Metadata.WrappedMasterKey) > 0}
	if t, err := time.Parse(time.RFC3339, f.Metadata.CreatedAt); err == nil {
		sum.Created = t
	}
	return data, sum, nil
}

// writeVaultFile writes data to path, readable only by the current user, by
// way of a temporary file beside it so a failure leaves path as it was.
func writeVaultFile(path string, data []byte) error {
	if err := secfile.MkdirAll(filepath.Dir(path)); err != nil {
		return &OpError{Code: CodeLocalIO, Msg: "create " + utils.DisplayPath(filepath.Dir(path)) + ": " + err.Error(), Err: err}
	}
	tmp := path + ".tmp"
	if err := secfile.WriteFile(tmp, data); err != nil {
		_ = os.Remove(tmp)
		return &OpError{Code: CodeLocalIO, Msg: "write " + utils.DisplayPath(path) + ": " + err.Error(), Err: err}
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return &OpError{Code: CodeLocalIO, Msg: "write " + utils.DisplayPath(path) + ": " + err.Error(), Err: err}
	}
	return nil
}

// samePath reports whether a and b name the same file, ignoring case where
// the file system does.
func samePath(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	if goruntime.GOOS == "windows" {
		return strings.EqualFold(aa, bb)
	}
	return aa == bb
}

// Backup copies the vault file at src to dst, after checking it is a vault.
// The copy is as safe to keep as the vault: its keys stay encrypted under the
// passphrase, and the file is readable only by the current user. A running
// agent saves every change as it makes it, so the file is current.
func Backup(src, dst string) (FileSummary, error) {
	if samePath(src, dst) {
		return FileSummary{}, &OpError{Code: CodeLocalIO, Msg: "a backup must go somewhere other than the vault itself"}
	}
	data, sum, err := readVaultFile(src)
	if err != nil {
		return FileSummary{}, err
	}
	return sum, writeVaultFile(dst, data)
}

// CheckVaultFile reports what the vault file at path holds and whether
// passphrase opens it, without unlocking or changing anything. A wrong
// passphrase is [CodeWrongPassphrase].
func CheckVaultFile(path string, passphrase []byte) (FileSummary, error) {
	_, sum, err := readVaultFile(path)
	if err != nil {
		return FileSummary{}, err
	}
	store, err := vault.Open(path)
	if err != nil {
		return FileSummary{}, &OpError{Code: CodeVaultUnreadable, Msg: err.Error(), Err: err}
	}
	if !vault.VerifyPassphrase(store, passphrase) {
		return sum, &OpError{Code: CodeWrongPassphrase, Msg: "that passphrase does not open " + utils.DisplayPath(path)}
	}
	return sum, nil
}

// ImportResult says where an imported vault went and where what it replaced
// was moved to.
type ImportResult struct {
	VaultPath string
	FileSummary
	// Previous is where the vault that was there before now is, or "" when
	// there was none.
	Previous string
	// PreviousRecovery is where that vault's recovery.txt now is, or "": the
	// phrase in it does not open the imported vault, so it does not stay
	// beside it under that name.
	PreviousRecovery string
}

// Import makes the vault file at src env's vault, after checking passphrase
// opens it. A vault already there is renamed to vault.json.bak-<time> rather
// than overwritten.
//
// The agent must not be serving a vault while this runs: it holds its vault in
// memory and would write it back over the import on its next change. Import
// refuses with [CodeVaultInUse] when a vault agent answers; the front end
// stops the agent first and starts it again after.
func Import(env Env, src string, passphrase []byte) (ImportResult, error) {
	dst := env.vaultFile()
	if dst == "" {
		return ImportResult{}, &OpError{
			Code: CodeNoVaultPath,
			Msg:  "vault path required",
			Hint: "Set [vault].vault_path in config, or pass --vault-path.",
		}
	}
	if samePath(src, dst) {
		return ImportResult{}, &OpError{Code: CodeVaultExists, Msg: utils.DisplayPath(src) + " is already your vault"}
	}
	data, sum, err := readVaultFile(src)
	if err != nil {
		return ImportResult{}, err
	}
	if _, err := CheckVaultFile(src, passphrase); err != nil {
		return ImportResult{}, err
	}
	if err := refuseWhileServed(env); err != nil {
		return ImportResult{}, err
	}

	res := ImportResult{VaultPath: dst, FileSummary: sum}
	stamp := time.Now().Format("20060102-150405")
	if _, err := os.Stat(dst); err == nil {
		res.Previous = dst + ".bak-" + stamp
		if err := os.Rename(dst, res.Previous); err != nil {
			return ImportResult{}, &OpError{Code: CodeLocalIO, Msg: "move the current vault aside: " + err.Error(), Err: err}
		}
	}
	if err := writeVaultFile(dst, data); err != nil {
		if res.Previous != "" {
			_ = os.Rename(res.Previous, dst) // put the old vault back
		}
		return ImportResult{}, err
	}
	recovery := filepath.Join(filepath.Dir(dst), "recovery.txt")
	if _, err := os.Stat(recovery); err == nil {
		moved := recovery + ".bak-" + stamp
		if os.Rename(recovery, moved) == nil {
			res.PreviousRecovery = moved
		}
	}
	return res, nil
}

// refuseWhileServed fails when a vault agent answers on env's socket.
func refuseWhileServed(env Env) error {
	c, err := open(env, "import", agentIfRunning)
	if err != nil {
		return err
	}
	defer c.close()
	if c.session == nil || c.backend.Mode != "vault" {
		return nil
	}
	return &OpError{
		Code: CodeVaultInUse,
		Msg:  "the agent is serving a vault, and would write it back over the import",
		Hint: "Stop the agent ('sshush stop'), import, then start it again.",
	}
}

// String is the summary as one line, for a confirmation or a status.
func (s FileSummary) String() string {
	text := fmt.Sprintf("%d key", s.Keys)
	if s.Keys != 1 {
		text += "s"
	}
	if s.Recovery {
		text += ", recovery phrase set up"
	}
	return text
}
