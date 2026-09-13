package platform

import (
	"path/filepath"
	"testing"
)

func TestServerLogPath_PrefersTheConfiguredPath(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/state")
	if got := ServerLogPath("  /var/log/sshush.log "); got != "/var/log/sshush.log" {
		t.Errorf("ServerLogPath(configured) = %q, want /var/log/sshush.log", got)
	}
}

func TestServerLogPath_UsesXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/state")
	if got, want := ServerLogPath(""), filepath.Join("/state", "sshush", "server.log"); got != want {
		t.Errorf("ServerLogPath(\"\") = %q, want %q", got, want)
	}
}

func TestServerLogPath_FallsBackToTheConfigDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	if got, want := ServerLogPath(""), filepath.Join("/cfg", "sshush", "server.log"); got != want {
		t.Errorf("ServerLogPath(\"\") = %q, want %q", got, want)
	}
}
