//go:build !windows

package tray

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vaultops"
)

// renameTarget adds a key from a file to a served vault and returns what
// the main window would show for it.
func renameTarget(t *testing.T, comment string) (vaultops.Env, vaultops.IdentityView) {
	t.Helper()
	sock := serveVault(t)
	path, fp := writeKey(t, comment, "")
	writePub(t, path, comment)
	if _, err := AddKeys(sock, []string{path}, nil); err != nil {
		t.Fatal(err)
	}
	env := vaultops.Env{SocketPath: sock}
	res, err := vaultops.List(env)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range res.Identities {
		if id.Fingerprint == fp {
			return env, id
		}
	}
	t.Fatalf("%s not in the vault", fp)
	return env, vaultops.IdentityView{}
}

func vaultComment(t *testing.T, env vaultops.Env, fp string) string {
	t.Helper()
	res, err := vaultops.List(env)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range res.Identities {
		if id.Fingerprint == fp {
			return id.Comment
		}
	}
	return ""
}

func TestRenameKeyRewritesTheFileAndTheVault(t *testing.T) {
	env, id := renameTarget(t, "old")
	status, err := RenameKey(env, nil, id, "new")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "key file") {
		t.Errorf("status: got %q", status)
	}
	if got := vaultComment(t, env, id.Fingerprint); got != "new" {
		t.Errorf("vault comment: got %q", got)
	}
	pub, _ := os.ReadFile(utils.LocalPath(id.Filepath) + ".pub")
	if !strings.HasSuffix(strings.TrimSpace(string(pub)), " new") {
		t.Errorf(".pub: got %q", pub)
	}
	if got := listComments(t, env.SocketPath); len(got) != 1 || got[0] != "new" {
		t.Errorf("agent: got %v", got)
	}
}

func TestRenameKeyLeavesAFileHoldingAnotherKeyAlone(t *testing.T) {
	env, id := renameTarget(t, "mine")
	other, _ := writeKey(t, "other", "")
	data, _ := os.ReadFile(other)
	if err := os.WriteFile(utils.LocalPath(id.Filepath), data, 0o600); err != nil { // the file was regenerated since
		t.Fatal(err)
	}
	status, err := RenameKey(env, nil, id, "renamed")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "left alone") {
		t.Errorf("status: got %q", status)
	}
	if after, _ := os.ReadFile(utils.LocalPath(id.Filepath)); string(after) != string(data) {
		t.Error("the file holding another key was rewritten")
	}
	if got := vaultComment(t, env, id.Fingerprint); got != "renamed" {
		t.Errorf("vault comment: got %q", got)
	}
}

func TestRenameKeyWithTheFileGone(t *testing.T) {
	env, id := renameTarget(t, "gone")
	os.Remove(id.Filepath)
	os.Remove(id.Filepath + ".pub")
	if _, err := RenameKey(env, nil, id, "still"); err != nil {
		t.Fatal(err)
	}
	if got := vaultComment(t, env, id.Fingerprint); got != "still" {
		t.Errorf("vault comment: got %q", got)
	}
}

func TestRenameKeyRejectsAndSkips(t *testing.T) {
	env, id := renameTarget(t, "same")
	var invalid *invalidCommentError
	if _, err := RenameKey(env, nil, id, "two\nlines"); !errors.As(err, &invalid) {
		t.Errorf("multi-line name: got %v", err)
	}
	if status, err := RenameKey(env, nil, id, "same"); err != nil || status != "" {
		t.Errorf("unchanged name: got %q, %v", status, err)
	}
}
