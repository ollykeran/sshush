package keys

import (
	"os"
	"os/user"
)

// DefaultComment returns a best-effort "user@host" comment.
func DefaultComment() string {
	u, err := user.Current()
	name := "user"
	if err == nil {
		name = u.Username
	}

	host, err := os.Hostname()
	if err != nil {
		host = "localhost"
	}

	return name + "@" + host
}
