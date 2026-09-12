package server

import (
	"net"
	"strings"
	"testing"
)

// envValues collects env into a map, later entries winning as they do for exec.
func envValues(env []string) map[string]string {
	values := make(map[string]string, len(env))
	for _, kv := range env {
		key, value, _ := strings.Cut(kv, "=")
		values[key] = value
	}
	return values
}

func TestSessionEnv_DescribesTheConnection(t *testing.T) {
	remote := &net.TCPAddr{IP: net.ParseIP("203.0.113.5"), Port: 50022}
	local := &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 2222}

	got := envValues(sessionEnv([]string{"PATH=/usr/bin"}, remote, local, "/bin/zsh"))

	if want := "203.0.113.5 50022 2222"; got["SSH_CLIENT"] != want {
		t.Errorf("SSH_CLIENT = %q, want %q", got["SSH_CLIENT"], want)
	}
	if want := "203.0.113.5 50022 192.0.2.10 2222"; got["SSH_CONNECTION"] != want {
		t.Errorf("SSH_CONNECTION = %q, want %q", got["SSH_CONNECTION"], want)
	}
	if got["SHELL"] != "/bin/zsh" {
		t.Errorf("SHELL = %q, want /bin/zsh", got["SHELL"])
	}
	if got["PATH"] != "/usr/bin" {
		t.Errorf("PATH = %q, want the daemon's own value kept", got["PATH"])
	}
}

func TestSessionEnv_PrintsIPv6HostsWithoutBrackets(t *testing.T) {
	addr := &net.TCPAddr{IP: net.ParseIP("::1"), Port: 2222}
	got := envValues(sessionEnv(nil, addr, addr, "/bin/sh"))
	if want := "::1 2222 ::1 2222"; got["SSH_CONNECTION"] != want {
		t.Errorf("SSH_CONNECTION = %q, want %q", got["SSH_CONNECTION"], want)
	}
}

// TestSessionEnv_DropsAnInheritedConnection covers a daemon started from a
// terminal inside an SSH login: neither must leak into the sessions it serves.
func TestSessionEnv_DropsAnInheritedConnection(t *testing.T) {
	base := []string{
		"SSH_CLIENT=198.51.100.1 1 22",
		"SSH_CONNECTION=198.51.100.1 1 198.51.100.2 22",
		"SSH_TTY=/dev/pts/9",
		"TERM=xterm-kitty",
		"SHELL=/usr/bin/fish",
	}
	env := sessionEnv(base, nil, nil, "/bin/sh")
	for _, kv := range env {
		if hasEnvKey(kv, perSessionEnvKeys...) {
			t.Errorf("env still has %q with no connection to describe", kv)
		}
	}
	if got := envValues(env)["SHELL"]; got != "/bin/sh" {
		t.Errorf("SHELL = %q, want the session's shell to win over the inherited one", got)
	}
}

func TestSessionEnv_FillsInTheUserOnlyWhenMissing(t *testing.T) {
	kept := envValues(sessionEnv([]string{"HOME=/elsewhere", "USER=someone"}, nil, nil, "/bin/sh"))
	if kept["HOME"] != "/elsewhere" || kept["USER"] != "someone" {
		t.Errorf("HOME, USER = %q, %q; want the daemon's own values kept", kept["HOME"], kept["USER"])
	}

	filled := envValues(sessionEnv(nil, nil, nil, "/bin/sh"))
	for _, key := range []string{"USER", "LOGNAME", "HOME"} {
		if filled[key] == "" {
			t.Errorf("%s is empty, want it filled in from the current user", key)
		}
	}
}
