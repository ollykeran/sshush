package platform

import (
	"path/filepath"
	"testing"
)

func TestConfigDir_respectsXDG_CONFIG_HOME(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdgcfg"))
	t.Setenv("XDG_RUNTIME_DIR", "")

	want := filepath.Join(tmp, "xdgcfg", "sshush")
	if got := ConfigDir(); got != want {
		t.Fatalf("ConfigDir: got %q, want %q", got, want)
	}
}

func TestRuntimeDataDir_fallsBackToConfigDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", "")

	cfgDir := filepath.Join(tmp, ".config", "sshush")
	if got := RuntimeDataDir(); got != cfgDir {
		t.Fatalf("RuntimeDataDir: got %q, want %q", got, cfgDir)
	}
}

func TestRuntimeDataDir_prefersXDG_RUNTIME_DIR(t *testing.T) {
	tmp := t.TempDir()
	rt := filepath.Join(tmp, "run")
	t.Setenv("XDG_RUNTIME_DIR", rt)

	if got := RuntimeDataDir(); got != rt {
		t.Fatalf("RuntimeDataDir: got %q, want %q", got, rt)
	}
}

func TestDefaultSocketPath_absoluteWithoutXDG(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", "")

	p := DefaultSocketPath()
	if !filepath.IsAbs(p) {
		t.Fatalf("expected absolute path, got %q", p)
	}
	if filepath.Base(p) != SocketFileName {
		t.Fatalf("basename: got %q, want %q", filepath.Base(p), SocketFileName)
	}
}

func TestServerHostKeyPath_usesTheConfiguredPath(t *testing.T) {
	if got, want := ServerHostKeyPath("/etc/sshush/host_key"), "/etc/sshush/host_key"; got != want {
		t.Fatalf("ServerHostKeyPath: got %q, want %q", got, want)
	}
}

func TestServerHostKeyPath_fallsBackToTheConfigDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", "")
	// The runtime dir is deliberately not used: a host key there would not survive
	// a reboot, and every returning client would see a host key change.
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(tmp, "run"))

	want := filepath.Join(tmp, ".config", "sshush", ServerHostKeyFileName)
	if got := ServerHostKeyPath("  "); got != want {
		t.Fatalf("ServerHostKeyPath: got %q, want %q", got, want)
	}
}
