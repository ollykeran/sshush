package keys

import (
	"os"
	"os/user"
	"strings"
)

// DefaultComment returns a best-effort "user@host" comment.
func DefaultComment() string {
	name := "user"
	if u, err := user.Current(); err == nil {
		name = accountName(u.Username)
	}

	host, err := os.Hostname()
	if err != nil {
		host = "localhost"
	}

	return name + "@" + host
}

// accountName drops the domain from a Windows account, DOMAIN\name, as
// ssh-keygen does, so the comment reads name@host rather than naming the
// machine twice.
func accountName(username string) string {
	if i := strings.LastIndexByte(username, '\\'); i >= 0 {
		return username[i+1:]
	}
	return username
}
