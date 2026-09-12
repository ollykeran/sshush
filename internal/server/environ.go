package server

import (
	"net"
	"os/user"
	"strings"
)

// perSessionEnvKeys are the variables that describe one session's connection and
// terminal. A daemon started from a terminal, or from inside an SSH login, inherits
// that terminal's values, which would be wrong for every session it serves, so they
// are always replaced or dropped. A session without a pty has no TERM, as with sshd.
var perSessionEnvKeys = []string{"SSH_CLIENT", "SSH_CONNECTION", "SSH_TTY", "TERM"}

// sessionEnv builds a session's environment the way sshd would: the daemon's own
// environment, with SHELL naming the shell the session runs, SSH_CLIENT and
// SSH_CONNECTION describing this connection, and USER, LOGNAME and HOME filled in
// if the daemon was started without them (as a service manager may). TERM and
// SSH_TTY are added separately, once there is a pty for them to describe.
func sessionEnv(base []string, remote, local net.Addr, shell string) []string {
	env := make([]string, 0, len(base)+6)
	for _, kv := range base {
		if !hasEnvKey(kv, perSessionEnvKeys...) {
			env = append(env, kv)
		}
	}

	if u, err := user.Current(); err == nil {
		env = appendIfUnset(env, "USER", u.Username)
		env = appendIfUnset(env, "LOGNAME", u.Username)
		env = appendIfUnset(env, "HOME", u.HomeDir)
	}
	env = append(env, "SHELL="+shell)

	clientHost, clientPort, clientOK := splitAddr(remote)
	serverHost, serverPort, serverOK := splitAddr(local)
	if clientOK && serverOK {
		env = append(env,
			"SSH_CLIENT="+clientHost+" "+clientPort+" "+serverPort,
			"SSH_CONNECTION="+clientHost+" "+clientPort+" "+serverHost+" "+serverPort,
		)
	}
	return env
}

// splitAddr splits a TCP address into the bare host and port sshd prints, with no
// brackets around an IPv6 host.
func splitAddr(addr net.Addr) (host, port string, ok bool) {
	if addr == nil {
		return "", "", false
	}
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "", "", false
	}
	return host, port, true
}

// appendIfUnset adds key=value unless env already sets key, even to empty.
func appendIfUnset(env []string, key, value string) []string {
	for _, kv := range env {
		if hasEnvKey(kv, key) {
			return env
		}
	}
	if value == "" {
		return env
	}
	return append(env, key+"="+value)
}

// hasEnvKey reports whether the key=value entry kv sets one of keys.
func hasEnvKey(kv string, keys ...string) bool {
	name, _, _ := strings.Cut(kv, "=")
	for _, key := range keys {
		if name == key {
			return true
		}
	}
	return false
}
