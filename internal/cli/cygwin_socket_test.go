package cli

import (
	"runtime"
	"testing"

	"github.com/ollykeran/sshush/internal/config"
)

func TestAuthSockFor(t *testing.T) {
	const pipe, cyg = `\\.\pipe\openssh-ssh-agent`, `C:\Users\me\.config\sshush\cygwin-agent.sock`
	for _, tc := range []struct {
		shell, cygwin, windows, other string
	}{
		{"", cyg, "C:/Users/me/.config/sshush/cygwin-agent.sock", pipe},
		{"bash", cyg, "C:/Users/me/.config/sshush/cygwin-agent.sock", pipe},
		{"fish", cyg, "C:/Users/me/.config/sshush/cygwin-agent.sock", pipe},
		{"powershell", cyg, pipe, pipe},
		{"pwsh", cyg, pipe, pipe},
		{"bash", "", pipe, pipe},
	} {
		want := tc.other
		if runtime.GOOS == "windows" {
			want = tc.windows
		}
		if got := authSockFor(tc.shell, pipe, tc.cygwin); got != want {
			t.Errorf("shell %q, cygwin %q: got %q, want %q", tc.shell, tc.cygwin, got, want)
		}
	}
}

func TestCygwinSocketOf_onlyForAnAgentSshushRuns(t *testing.T) {
	for typ, want := range map[string]string{
		config.AgentTypeKeys: "x", config.AgentTypeVault: "x", config.AgentTypeExternal: "",
	} {
		if got := cygwinSocketOf(config.Config{AgentType: typ, CygwinSocket: "x"}); got != want {
			t.Errorf("%s: got %q, want %q", typ, got, want)
		}
	}
}
