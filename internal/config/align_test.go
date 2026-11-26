package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAlignIdentityAgent(t *testing.T) {
	stale := func(v string) bool { return samePipe(v, `\\.\pipe\sshush-agent`) }
	cases := []struct {
		name, in, want string
		changed        bool
	}{
		{"rewrites the stale pipe, keeping CRLF and indent",
			"Host x\r\n  HostName x\r\n\r\n# sshush: use the sshush agent\r\nHost *\r\n    IdentityAgent //./pipe/sshush-agent\r\n",
			"Host x\r\n  HostName x\r\n\r\n# sshush: use the sshush agent\r\nHost *\r\n    IdentityAgent //./pipe/openssh-ssh-agent\r\n", true},
		{"backslashes and no final newline",
			`IdentityAgent \\.\pipe\sshush-agent`, "IdentityAgent //./pipe/openssh-ssh-agent", true},
		{"leaves another agent alone",
			"Host *\n    IdentityAgent //./pipe/openssh-ssh-agent-1password\n", "Host *\n    IdentityAgent //./pipe/openssh-ssh-agent-1password\n", false},
		{"leaves a commented line alone",
			"# IdentityAgent //./pipe/sshush-agent\n", "# IdentityAgent //./pipe/sshush-agent\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			if err := os.WriteFile(path, []byte(c.in), 0o600); err != nil {
				t.Fatal(err)
			}
			changed, err := alignIdentityAgent(path, `\\.\pipe\openssh-ssh-agent`, stale)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(path)
			if changed != c.changed || string(got) != c.want {
				t.Errorf("got changed=%v %q, want changed=%v %q", changed, got, c.changed, c.want)
			}
		})
	}
}

func TestAlignIdentityAgentNoFile(t *testing.T) {
	changed, err := alignIdentityAgent(filepath.Join(t.TempDir(), "missing"), `\\.\pipe\x`, func(string) bool { return true })
	if changed || err != nil {
		t.Errorf("got %v, %v", changed, err)
	}
}
