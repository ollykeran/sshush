package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/config"
)

// runStartCapturingStdout runs runStartDaemon against an already-running test
// agent with stdout replaced by a pipe, and returns what was written to it.
func runStartCapturingStdout(t *testing.T, shell string) (string, error) {
	t.Helper()
	return runStartWithConfigShell(t, "", shell)
}

// runStartWithConfigShell is runStartCapturingStdout with [agent].shell set to configShell.
func runStartWithConfigShell(t *testing.T, configShell, shell string) (string, error) {
	t.Helper()
	socketPath, _ := startTestAgent(t)
	configPath := writeConfigFile(t, "[agent]\nsocket_path = \""+socketPath+"\"\nkey_paths = []\n")

	cmd := cmdWithConfigFlag(configPath)
	withConfig(cmd, &config.Config{SocketPath: socketPath, Shell: configShell})
	addShellFlag(cmd)
	if shell != "" {
		if err := cmd.Flags().Set("shell", shell); err != nil {
			t.Fatal(err)
		}
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdout := os.Stdout
	os.Stdout = w
	runErr := runStartDaemon(cmd)
	os.Stdout = origStdout
	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out)) + "|" + socketPath, runErr
}

func TestStart_authSockLineDefaultIsPosix(t *testing.T) {
	got, err := runStartCapturingStdout(t, "")
	if err != nil {
		t.Fatal(err)
	}
	line, socketPath, _ := strings.Cut(got, "|")
	if want := "export SSH_AUTH_SOCK='" + socketPath + "'"; line != want {
		t.Fatalf("got %q, want %q", line, want)
	}
}

func TestStart_authSockLineFish(t *testing.T) {
	got, err := runStartCapturingStdout(t, "fish")
	if err != nil {
		t.Fatal(err)
	}
	line, socketPath, _ := strings.Cut(got, "|")
	if want := "set -gx SSH_AUTH_SOCK '" + socketPath + "';"; line != want {
		t.Fatalf("got %q, want %q", line, want)
	}
}

func TestStart_authSockLinePowerShell(t *testing.T) {
	got, err := runStartCapturingStdout(t, "powershell")
	if err != nil {
		t.Fatal(err)
	}
	line, socketPath, _ := strings.Cut(got, "|")
	if want := "$env:SSH_AUTH_SOCK = '" + socketPath + "'"; line != want {
		t.Fatalf("got %q, want %q", line, want)
	}
}

func TestStart_unsupportedShellErrors(t *testing.T) {
	got, err := runStartCapturingStdout(t, "nushell")
	if err == nil {
		t.Fatal("expected error for unsupported --shell")
	}
	if line, _, _ := strings.Cut(got, "|"); line != "" {
		t.Fatalf("expected nothing on stdout, got %q", line)
	}
}

func TestStart_authSockLineFromConfigShell(t *testing.T) {
	got, err := runStartWithConfigShell(t, "fish", "")
	if err != nil {
		t.Fatal(err)
	}
	line, socketPath, _ := strings.Cut(got, "|")
	if want := "set -gx SSH_AUTH_SOCK '" + socketPath + "';"; line != want {
		t.Fatalf("got %q, want %q", line, want)
	}
}

func TestStart_shellFlagOverridesConfigShell(t *testing.T) {
	got, err := runStartWithConfigShell(t, "fish", "posix")
	if err != nil {
		t.Fatal(err)
	}
	line, socketPath, _ := strings.Cut(got, "|")
	if want := "export SSH_AUTH_SOCK='" + socketPath + "'"; line != want {
		t.Fatalf("got %q, want %q", line, want)
	}
}
