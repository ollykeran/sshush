package tray

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/editcomment"
	"github.com/ollykeran/sshush/internal/keys"
	"github.com/ollykeran/sshush/internal/openssh"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
	"github.com/ollykeran/sshush/internal/vaultops"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// AskKeyPassphrase asks for the passphrase of the key file at path. wrong
// says the last one given did not decrypt it. It returns errCancelled when
// the user declines, and the caller zeroes what it returns.
type AskKeyPassphrase func(path string, wrong bool) ([]byte, error)

// errCancelled reports that the user dismissed a prompt or dialog.
var errCancelled = errors.New("tray: prompt cancelled")

// errSkipped reports a key file the user chose not to decrypt.
var errSkipped = errors.New("skipped: no passphrase given")

// AddKeys adds the private key files at paths to the agent, as 'sshush add'
// does: into the vault with autoload on when the agent is a vault, into the
// agent's memory otherwise. A passphrase-protected file is decrypted here,
// with the passphrase from ask, and the agent only ever sees the plain key.
// It tries every file and reports how many it added and, joined, why each of
// the others was not.
func AddKeys(socketPath string, paths []string, ask AskKeyPassphrase) (added int, err error) {
	s, err := agent.Open(socketPath)
	if err != nil {
		return 0, fmt.Errorf("the agent is not running: %w", err)
	}
	defer s.Close()
	backend, err := s.Backend()
	if err != nil {
		return 0, fmt.Errorf("the agent did not answer: %w", err)
	}
	inVault := backend.Mode == "vault"
	var errs []error
	for _, path := range paths {
		if addErr := addKey(s, inVault, path, ask); addErr != nil {
			errs = append(errs, fmt.Errorf("%s: %s", filepath.Base(path), describeAddError(addErr)))
			continue
		}
		added++
	}
	return added, errors.Join(errs...)
}

func addKey(s *agent.Session, inVault bool, path string, ask AskKeyPassphrase) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	defer clear(data)
	_, err = ssh.ParseRawPrivateKey(data)
	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) {
		// Not encrypted, or not a key at all: the usual path says which.
		if inVault {
			return vault.AddPrivateKeyFile(s, path, true)
		}
		return s.AddKeyFromPath(path)
	}

	key, err := decryptKey(data, path, ask)
	if err != nil {
		return err
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return err
	}
	comment := keyComment(path, signer.PublicKey())
	if !inVault {
		return s.Add(sshagent.AddedKey{PrivateKey: key, Comment: comment})
	}
	block, err := ssh.MarshalPrivateKey(key, comment)
	if err != nil {
		return err
	}
	defer clear(block.Bytes)
	plain := pem.EncodeToMemory(block)
	defer clear(plain)
	return vault.AddPrivateKeyPEM(s, plain, path, true)
}

// decryptKey asks for the passphrase until it decrypts data or the user gives up.
func decryptKey(data []byte, path string, ask AskKeyPassphrase) (any, error) {
	if ask == nil {
		return nil, openssh.ErrEncryptedPrivateKey
	}
	wrong := false
	for {
		pass, err := ask(path, wrong)
		if errors.Is(err, errCancelled) {
			return nil, errSkipped
		}
		if err != nil {
			return nil, err
		}
		key, err := ssh.ParseRawPrivateKeyWithPassphrase(data, pass)
		clear(pass)
		if errors.Is(err, x509.IncorrectPasswordError) {
			wrong = true
			continue
		}
		if err != nil {
			return nil, err
		}
		if k, ok := key.(*ed25519.PrivateKey); ok {
			key = *k // the form ssh.MarshalPrivateKey takes
		}
		return key, nil
	}
}

// keyComment names a decrypted key: an encrypted file hides its comment, so
// take the one in the .pub beside it when that is the same key, else the file
// name, as the agent does for a key without one.
func keyComment(path string, pub ssh.PublicKey) string {
	if data, err := os.ReadFile(path + ".pub"); err == nil {
		filePub, comment, _, _, err := ssh.ParseAuthorizedKey(data)
		if err == nil && comment != "" && ssh.FingerprintSHA256(filePub) == ssh.FingerprintSHA256(pub) {
			return comment
		}
	}
	return filepath.Base(path)
}

func describeAddError(err error) string {
	switch {
	case errors.Is(err, errSkipped):
		return err.Error()
	case errors.Is(err, openssh.ErrEncryptedPrivateKey):
		return "the key is protected by a passphrase"
	case errors.Is(err, agent.ErrVaultLocked):
		return "the vault is locked; unlock it first"
	}
	return err.Error()
}

// UnloadKey takes the key with fingerprint out of the running agent and
// deletes nothing. A vault keeps the identity, hidden until it is loaded
// again or the agent restarts; a keyring forgets the key, whose file is
// untouched.
func UnloadKey(socketPath, fingerprint string) error {
	s, err := agent.Open(socketPath)
	if err != nil {
		return fmt.Errorf("the agent is not running: %w", err)
	}
	defer s.Close()
	backend, err := s.Backend()
	if err != nil {
		return fmt.Errorf("the agent did not answer: %w", err)
	}
	if backend.Mode == "vault" {
		return vault.SessionUnload(s, fingerprint)
	}
	found, err := s.RemoveByFingerprint(fingerprint)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("the agent no longer holds %s", fingerprint)
	}
	return nil
}

// invalidCommentError is a name the key cannot take, said so the user can
// change it and try again.
type invalidCommentError struct{ msg string }

func (e *invalidCommentError) Error() string { return e.msg }

// RenameKey gives the key id the comment, as 'sshush edit --comment' and the
// TUI do: the key file it came from is rewritten with the new comment, its
// .pub too, and the running agent is told. A vault keeps its own name for
// the key as well, so a vault identity whose file is gone, or protected by a
// passphrase, is still renamed there. keyPaths are the config's, where a key
// in an agent that is not a vault may have come from. It returns what it did.
func RenameKey(env vaultops.Env, keyPaths []string, id vaultops.IdentityView, comment string) (string, error) {
	if err := editcomment.Validate(comment); err != nil {
		return "", &invalidCommentError{"A name cannot span lines."}
	}
	if comment == id.Comment {
		return "", nil
	}
	s, err := agent.Open(env.SocketPath)
	if err != nil {
		return "", fmt.Errorf("the agent is not running: %w", err)
	}
	defer s.Close()
	backend, err := s.Backend()
	if err != nil {
		return "", fmt.Errorf("the agent did not answer: %w", err)
	}
	inVault := backend.Mode == "vault"

	path := utils.LocalPath(id.Filepath)
	if path == "" && !inVault {
		path = agent.ResolveFilepath(id.Fingerprint, keyPaths)
	}
	fileErr := rewriteComment(path, id.Fingerprint, comment)
	named := fmt.Sprintf("Renamed %s to %q", identityName(id), comment)

	if fileErr == nil {
		res := editcomment.SyncAgent(s, id.Fingerprint, path, comment)
		switch {
		case res.VaultErr != nil:
			return "", fmt.Errorf("the key file was renamed, but the vault was not: %w", res.VaultErr)
		case res.ReloadErr != nil:
			return "", fmt.Errorf("the key file was renamed, but the agent still has the old name: %w", res.ReloadErr)
		}
		return named + ", and its key file " + utils.DisplayPath(path) + ".", nil
	}
	if !inVault {
		if path == "" {
			return "", errors.New("sshush does not know which file holds this key, so cannot rename it. " +
				"Add the file to [agent].key_paths, or use 'sshush edit <file> --comment'")
		}
		return "", fmt.Errorf("could not rename %s: %w", utils.DisplayPath(path), fileErr)
	}
	if err := vault.SetComment(s, id.Fingerprint, comment); err != nil {
		return "", err
	}
	if path == "" || errors.Is(fileErr, os.ErrNotExist) {
		return named + " in the vault.", nil
	}
	return named + " in the vault; its key file was left alone: " + describeAddError(fileErr) + ".", nil
}

// errOtherKey reports a key file that now holds a different key.
var errOtherKey = errors.New("the file now holds a different key")

// rewriteComment rewrites the private key file at path, and its .pub, with
// comment, after checking it still holds the key with fingerprint.
func rewriteComment(path, fingerprint, comment string) error {
	if path == "" {
		return os.ErrNotExist
	}
	_, raw, signer, err := keys.LoadKeyMaterial(path)
	if err != nil {
		return err
	}
	if ssh.FingerprintSHA256(signer.PublicKey()) != fingerprint {
		return errOtherKey
	}
	return keys.SaveWithComment(raw, comment, path)
}
