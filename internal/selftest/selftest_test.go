package selftest

import (
	"crypto/ed25519"
	"net"
	"path/filepath"
	"runtime"
	"testing"

	sshagent "golang.org/x/crypto/ssh/agent"
)

// serve runs kr on a Unix socket and returns its path.
func serve(t *testing.T, kr sshagent.Agent) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("serves on a Unix socket")
	}
	path := filepath.Join(t.TempDir(), "agent.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_ = sshagent.ServeAgent(kr, c)
			}()
		}
	}()
	return path
}

func names(r Report) []string {
	var n []string
	for _, c := range r.Checks {
		n = append(n, c.Name)
	}
	return n
}

func TestRunAllPass(t *testing.T) {
	kr := sshagent.NewKeyring()
	_, priv, _ := ed25519.GenerateKey(nil)
	if err := kr.Add(sshagent.AddedKey{PrivateKey: priv, Comment: "selftest"}); err != nil {
		t.Fatal(err)
	}
	sock := serve(t, kr)
	r := Run(sock, sock, "")
	if r.Worst() != OK {
		t.Fatalf("want every check OK, got %+v", r.Checks)
	}
	if got := names(r); len(got) != 6 || got[len(got)-1] != "sign" {
		t.Errorf("checks: got %v", got)
	}
}

func TestRunNoKeysWarnsAndStops(t *testing.T) {
	sock := serve(t, sshagent.NewKeyring())
	r := Run(sock, sock, "")
	last := r.Checks[len(r.Checks)-1]
	if last.Name != "list" || last.Status != Warn || r.Worst() != Warn {
		t.Errorf("got %+v", r.Checks)
	}
}

func TestRunMissingSocketHints(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "nothing.sock")
	r := Run(sock, sock, "")
	last := r.Checks[len(r.Checks)-1]
	if last.Name != "socket" || last.Status != Fail || last.Hint == "" {
		t.Errorf("got %+v", r.Checks)
	}
}

func TestAuthSockEnvLine(t *testing.T) {
	const sock = `\\.\pipe\sshush-agent`
	tests := []struct {
		name, authSock, persisted string
		want                      string
		wantOK                    bool
	}{
		{"set and right", sock, "", "SSH_AUTH_SOCK=" + sock + "  ✓", true},
		{"set and right, whatever is persisted", sock, "elsewhere", "SSH_AUTH_SOCK=" + sock + "  ✓", true},
		{"set to another agent", "/tmp/other.sock", "", "SSH_AUTH_SOCK=/tmp/other.sock (differs from socket)", false},
		{"unset", "", "", "SSH_AUTH_SOCK not set", false},
		{"unset here but set for new windows", "", sock, "SSH_AUTH_SOCK not set in this window; new windows have it", false},
		{"unset here, new windows get another agent", "", "elsewhere", "SSH_AUTH_SOCK not set in this window; new windows get elsewhere (differs from socket)", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := authSockEnvLine(tt.authSock, tt.persisted, sock)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("got %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
