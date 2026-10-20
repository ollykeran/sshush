package utils

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ssh "golang.org/x/crypto/ssh"
)

func mustWriteTestKey(t *testing.T, path string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestExpandHomeDirectory(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		path string
		want string
	}{
		{"home directory", "~/id_rsa", filepath.Join(homeDir, "id_rsa")},
		{"tilde only", "~", homeDir},
		{"tilde in middle unchanged", filepath.Join("a", "~", "b"), filepath.Join("a", "~", "b")},
		{"relative path", "./id_rsa", "./id_rsa"},
		{"absolute path", filepath.Join(homeDir, "id_rsa"), filepath.Join(homeDir, "id_rsa")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExpandHomeDirectory(tc.path)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestContractHomeDirectory(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		path string
		want string
	}{
		{"home directory", "~/id_rsa", "~/id_rsa"},
		{"relative path", "./id_rsa", "./id_rsa"},
		{"absolute path", filepath.Join(homeDir, "id_rsa"), filepath.Join("~", "id_rsa")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ContractHomeDirectory(tc.path)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDiscoverKeyPaths_SymlinkDedup(t *testing.T) {
	cases := []struct {
		name        string
		realName    string
		symlinkName string
	}{
		{"symlink sorts before target", "username@laptop.privatekey", "id_ed25519"},
		{"symlink sorts after target", "a_key", "z_link"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			realPath := filepath.Join(dir, tc.realName)
			symlinkPath := filepath.Join(dir, tc.symlinkName)
			mustWriteTestKey(t, realPath)
			if err := os.Symlink(realPath, symlinkPath); err != nil {
				t.Fatal(err)
			}

			paths := DiscoverKeyPaths([]string{dir}, false, false, false)
			if len(paths) != 1 {
				t.Fatalf("got %d paths, want 1: %+v", len(paths), paths)
			}
			kp := paths[0]
			if !kp.IsSymlink {
				t.Fatalf("IsSymlink = false, want true: %+v", kp)
			}
			if kp.Path != symlinkPath {
				t.Errorf("Path = %q, want %q", kp.Path, symlinkPath)
			}
			wantReal, err := filepath.EvalSymlinks(realPath)
			if err != nil {
				t.Fatal(err)
			}
			if kp.RealPath != wantReal {
				t.Errorf("RealPath = %q, want %q", kp.RealPath, wantReal)
			}
		})
	}
}

func TestDisplayPath(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	absUnderHome := filepath.Join(homeDir, "foo", "bar")
	got := DisplayPath(absUnderHome)
	want := filepath.Join("~", "foo", "bar")
	if got != want {
		t.Errorf("under home: got %q, want %q", got, want)
	}
	if got := DisplayPath(""); got != "" {
		t.Errorf("empty: got %q", got)
	}
}

func TestWSLPath(t *testing.T) {
	tests := []struct {
		in, distro, linux string
		ok                bool
	}{
		{`\\wsl.localhost\Debian\home\olly\.ssh\id_ed25519`, "Debian", "/home/olly/.ssh/id_ed25519", true},
		{`\\wsl$\Ubuntu-24.04\root\key`, "Ubuntu-24.04", "/root/key", true},
		{`\\WSL.LOCALHOST\Debian\tmp`, "Debian", "/tmp", true},
		{`//wsl.localhost/Debian/tmp/k`, "Debian", "/tmp/k", true},
		{`\\wsl.localhost\Debian`, "Debian", "/", true},
		{`\\wsl.localhost\`, "", "", false},
		{`\\server\share\key`, "", "", false},
		{`C:\Users\olly\.ssh\id_rsa`, "", "", false},
		{`/home/olly/.ssh/id_rsa`, "", "", false},
	}
	for _, tc := range tests {
		distro, linux, ok := WSLPath(tc.in)
		if distro != tc.distro || linux != tc.linux || ok != tc.ok {
			t.Errorf("WSLPath(%q) = %q, %q, %v; want %q, %q, %v", tc.in, distro, linux, ok, tc.distro, tc.linux, tc.ok)
		}
	}
}

func TestDisplayPathShowsWSLPathsAsLinux(t *testing.T) {
	if got := DisplayPath(`\\wsl.localhost\Debian\srv\keys\id_ed25519`); got != "/srv/keys/id_ed25519" {
		t.Errorf("got %q", got)
	}
	home, err := os.UserHomeDir()
	if err != nil || filepath.Separator != '/' {
		return // only a Linux home can lie inside a distro
	}
	if got := DisplayPath(`\\wsl.localhost\Debian` + strings.ReplaceAll(home, "/", `\`) + `\.ssh\id`); got != "~/.ssh/id" {
		t.Errorf("path under home: got %q", got)
	}
}
