package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ollykeran/sshush/internal/config"
)

func withEnv(t *testing.T, key, value string, fn func()) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if value == "" {
		_ = os.Unsetenv(key)
	} else {
		_ = os.Setenv(key, value)
	}
	defer func() {
		if had {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	}()
	fn()
}

func TestResolveSocketPath_ConfigWins(t *testing.T) {
	withEnv(t, "SSH_AUTH_SOCK", "/env/agent.sock", func() {
		withEnv(t, "XDG_RUNTIME_DIR", "/run/user/1000", func() {
			cfg := config.Config{SocketPath: "/from/config.sock"}
			got, err := ResolveSocketPath(&cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got != "/from/config.sock" {
				t.Fatalf("got %q, want /from/config.sock", got)
			}
		})
	})
}

func TestResolveSocketPath_UsesSSHAuthSock(t *testing.T) {
	withEnv(t, "SSH_AUTH_SOCK", "/env/agent.sock", func() {
		withEnv(t, "XDG_RUNTIME_DIR", "/run/user/1000", func() {
			got, err := ResolveSocketPath(nil)
			if err != nil {
				t.Fatal(err)
			}
			if got != "/env/agent.sock" {
				t.Fatalf("got %q, want /env/agent.sock", got)
			}
		})
	})
}

func TestResolveSocketPath_UsesXDGRuntimeDir(t *testing.T) {
	withEnv(t, "SSH_AUTH_SOCK", "", func() {
		withEnv(t, "XDG_RUNTIME_DIR", "/run/user/1000", func() {
			got, err := ResolveSocketPath(nil)
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join("/run/user/1000", "sshush.sock")
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	})
}

func TestResolveSocketPath_FallbackWhenNoXDG(t *testing.T) {
	tmp := t.TempDir()
	withEnv(t, "HOME", tmp, func() {
		withEnv(t, "SSH_AUTH_SOCK", "", func() {
			withEnv(t, "XDG_RUNTIME_DIR", "", func() {
				withEnv(t, "XDG_CONFIG_HOME", "", func() {
					got, err := ResolveSocketPath(nil)
					if err != nil {
						t.Fatal(err)
					}
					want := filepath.Join(tmp, ".config", "sshush", "sshush.sock")
					if got != want {
						t.Fatalf("got %q, want %q", got, want)
					}
				})
			})
		})
	})
}

func TestSocketPathForSSHushGUI_IgnoresSSHAuthSock(t *testing.T) {
	withEnv(t, "SSH_AUTH_SOCK", "/wrong/gnome-keyring/ssh", func() {
		withEnv(t, "XDG_RUNTIME_DIR", "/run/user/1000", func() {
			got, err := SocketPathForSSHushGUI(nil)
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join("/run/user/1000", "sshush.sock")
			if got != want {
				t.Fatalf("got %q, want %q (must not use SSH_AUTH_SOCK)", got, want)
			}
		})
	})
}

func TestSocketPathForSSHushGUI_ConfigWins(t *testing.T) {
	withEnv(t, "SSH_AUTH_SOCK", "/env/other.sock", func() {
		withEnv(t, "XDG_RUNTIME_DIR", "/run/user/1000", func() {
			cfg := &config.Config{SocketPath: "/custom/sshush.sock"}
			got, err := SocketPathForSSHushGUI(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got != "/custom/sshush.sock" {
				t.Fatalf("got %q", got)
			}
		})
	})
}

func TestPidFilePath_UsesXDGRuntimeDir(t *testing.T) {
	withEnv(t, "XDG_RUNTIME_DIR", "/run/user/1000", func() {
		got := PidFilePath()
		want := filepath.Join("/run/user/1000", "sshush.pid")
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

func TestPidFilePath_FallbackWithoutXDG(t *testing.T) {
	tmp := t.TempDir()
	withEnv(t, "HOME", tmp, func() {
		withEnv(t, "XDG_RUNTIME_DIR", "", func() {
			withEnv(t, "XDG_CONFIG_HOME", "", func() {
				got := PidFilePath()
				want := filepath.Join(tmp, ".config", "sshush", "sshush.pid")
				if got != want {
					t.Fatalf("got %q, want %q", got, want)
				}
			})
		})
	})
}

