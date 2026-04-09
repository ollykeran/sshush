package server

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

// FileAuth implements AuthKeySource by reading authorized keys from a file (e.g. OpenSSH authorized_keys format).
// The file is read at construction. Changes to the file require a new FileAuth.
type FileAuth struct {
	blobs [][]byte
}

// NewFileAuth reads the file at path and parses it as authorized_keys (one key per line, ssh.ParseAuthorizedKey).
// Returns an error if the file cannot be read; invalid lines are skipped.
func NewFileAuth(path string) (*FileAuth, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var blobs [][]byte
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			continue
		}
		blobs = append(blobs, pub.Marshal())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return &FileAuth{blobs: blobs}, nil
}

// Authorized returns true if the given public key matches one of the keys in the file.
func (f *FileAuth) Authorized(key ssh.PublicKey) bool {
	clientBlob := key.Marshal()
	for _, b := range f.blobs {
		if len(b) == len(clientBlob) && subtle.ConstantTimeCompare(b, clientBlob) == 1 {
			return true
		}
	}
	return false
}
