package tray

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ollykeran/sshush/internal/editcomment"
	"github.com/ollykeran/sshush/internal/keys"
	"github.com/ollykeran/sshush/internal/utils"
)

// keyChoice is one kind of key the create dialog offers, as 'sshush create'
// takes it.
type keyChoice struct {
	label   string
	keyType string
	bits    int
}

// keyChoices are what the create dialog offers, the recommended one first.
var keyChoices = []keyChoice{
	{"ED25519 (recommended)", "ed25519", 0},
	{"RSA 4096", "rsa", 4096},
	{"RSA 3072", "rsa", 3072},
	{"ECDSA P-256", "ecdsa", 256},
	{"ECDSA P-384", "ecdsa", 384},
}

// errKeyExists reports a key file that would be overwritten.
var errKeyExists = errors.New("a file with that name already exists; choose another name")

// suggestKeyPath is where a new key of keyType goes by default:
// ~/.ssh/id_<type>, or id_<type>_2 and so on when that is taken.
func suggestKeyPath(sshDir, keyType string) string {
	base := filepath.Join(sshDir, "id_"+keyType)
	path := base
	for n := 2; exists(path) || exists(path+".pub"); n++ {
		path = fmt.Sprintf("%s_%d", base, n)
	}
	return path
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// CreateKey makes a new key pair of the kind at choice, as 'sshush create'
// does, and writes it to path and path.pub, refusing to overwrite either.
func CreateKey(choice int, comment, path string) error {
	if choice < 0 || choice >= len(keyChoices) {
		return errors.New("choose a kind of key")
	}
	if err := editcomment.Validate(comment); err != nil {
		return errors.New("the name cannot span lines")
	}
	path = utils.ExpandHomeDirectory(strings.TrimSpace(path))
	if path == "" || strings.HasSuffix(path, string(filepath.Separator)) {
		return errors.New("choose where to save the key")
	}
	if exists(path) || exists(path+".pub") {
		return errKeyExists
	}
	k := keyChoices[choice]
	priv, pub, err := keys.Generate(k.keyType, k.bits, comment)
	if err != nil {
		return err
	}
	defer clear(priv)
	return keys.SavePair(filepath.Dir(path), filepath.Base(path), priv, pub)
}
