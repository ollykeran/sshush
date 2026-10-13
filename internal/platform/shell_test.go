package platform

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestShellRcPathForAutoSetup_zsh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shells; Windows sets up a PowerShell profile")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("SHELL", "/bin/zsh")

	got, ok := ShellRcPathForAutoSetup()
	if !ok {
		t.Fatal("expected ok")
	}
	want := filepath.Join(tmp, ".zshrc")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestShellRcPathForAutoSetup_bash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shells; Windows sets up a PowerShell profile")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("SHELL", "/usr/bin/bash")

	got, ok := ShellRcPathForAutoSetup()
	if !ok {
		t.Fatal("expected ok")
	}
	want := filepath.Join(tmp, ".bashrc")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestShellSetupForAutoSetup_fish(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shells; Windows sets up a PowerShell profile")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("SHELL", "/usr/bin/fish")
	t.Setenv("XDG_CONFIG_HOME", "")

	got, ok := ShellSetupForAutoSetup()
	if !ok {
		t.Fatal("expected ok")
	}
	want := filepath.Join(tmp, ".config", "fish", "conf.d", "sshush.fish")
	if got.RcPath != want {
		t.Fatalf("got %q, want %q", got.RcPath, want)
	}
	if !got.Fish || got.Snippet != FishSnippet {
		t.Fatalf("expected fish snippet, got %+v", got)
	}
}

func TestShellSetupForAutoSetup_fishXDGConfigHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shells; Windows sets up a PowerShell profile")
	}
	tmp := t.TempDir()
	xdg := filepath.Join(tmp, "xdg")
	t.Setenv("HOME", tmp)
	t.Setenv("SHELL", "/usr/bin/fish")
	t.Setenv("XDG_CONFIG_HOME", xdg)

	got, ok := ShellSetupForAutoSetup()
	if !ok {
		t.Fatal("expected ok")
	}
	want := filepath.Join(xdg, "fish", "conf.d", "sshush.fish")
	if got.RcPath != want {
		t.Fatalf("got %q, want %q", got.RcPath, want)
	}
}

func TestShellSetupForAutoSetup_bashSnippet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shells; Windows sets up a PowerShell profile")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/usr/bin/bash")

	got, ok := ShellSetupForAutoSetup()
	if !ok {
		t.Fatal("expected ok")
	}
	if got.Fish || got.Snippet != EvalLine {
		t.Fatalf("expected eval line, got %+v", got)
	}
}

func TestAuthSockLine(t *testing.T) {
	tests := []struct {
		name, shell, socket, want string
	}{
		{"default is posix", "", "/run/user/1000/sshush.sock", "export SSH_AUTH_SOCK='/run/user/1000/sshush.sock'"},
		{"posix", "posix", "/tmp/a.sock", "export SSH_AUTH_SOCK='/tmp/a.sock'"},
		{"bash alias", "bash", "/tmp/a.sock", "export SSH_AUTH_SOCK='/tmp/a.sock'"},
		{"zsh alias", "ZSH", "/tmp/a.sock", "export SSH_AUTH_SOCK='/tmp/a.sock'"},
		{"posix quote", "posix", "/tmp/o'neil/a.sock", `export SSH_AUTH_SOCK='/tmp/o'\''neil/a.sock'`},
		{"fish", "fish", "/run/user/1000/sshush.sock", "set -gx SSH_AUTH_SOCK '/run/user/1000/sshush.sock';"},
		{"powershell pipe", "powershell", `\\.\pipe\sshush-agent`, `$env:SSH_AUTH_SOCK = '\\.\pipe\sshush-agent'`},
		{"pwsh alias", "pwsh", `\\.\pipe\sshush-agent`, `$env:SSH_AUTH_SOCK = '\\.\pipe\sshush-agent'`},
		{"powershell quote", "powershell", `C:\o'neil\a.sock`, `$env:SSH_AUTH_SOCK = 'C:\o''neil\a.sock'`},
		{"fish quote and backslash", "fish", `/tmp/o'neil\a.sock`, `set -gx SSH_AUTH_SOCK '/tmp/o\'neil\\a.sock';`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := AuthSockLine(tt.shell, tt.socket)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAuthSockLine_unsupportedShell(t *testing.T) {
	if _, err := AuthSockLine("nushell", "/tmp/a.sock"); err == nil {
		t.Fatal("expected error for unsupported shell")
	}
}

func TestDetectShell(t *testing.T) {
	tests := []struct {
		name, parent, shellEnv, want string
	}{
		{"parent fish wins over login bash", "fish", "/bin/bash", "fish"},
		{"parent bash wins over login fish", "bash", "/usr/bin/fish", "bash"},
		{"login shell dash prefix", "-zsh", "", "zsh"},
		{"non-shell parent falls back to SHELL", "go", "/usr/bin/fish", "fish"},
		{"dash is posix", "dash", "", "posix"},
		{"windows powershell", "powershell.exe", "", "powershell"},
		{"powershell 7", "pwsh.exe", "", "powershell"},
		{"git bash on windows", "bash.exe", `C:\Program Files\Git\usr\bin\bash.exe`, "bash"},
		{"windows path in SHELL", "cmd.exe", `C:\Program Files\Git\usr\bin\bash.exe`, "bash"},
		{"nothing known", "systemd", "/usr/bin/nu", fallbackShell},
		{"empty", "", "", fallbackShell},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectShell(tt.parent, tt.shellEnv)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			if !ValidShell(got) {
				t.Fatalf("detected shell %q is not accepted by AuthSockLine", got)
			}
		})
	}
}

func TestFishConfigStartsAgent(t *testing.T) {
	tests := []struct {
		name, content string
		want          bool
	}{
		{"eval", "if status is-interactive\n  eval (sshush start)\nend\n", true},
		{"pipe to source", "sshush start --shell fish | source\n", true},
		{"completion only", "sshush completion fish | source\n", false},
		{"commented out", "# eval (sshush start)\n", false},
		{"unrelated", "zoxide init fish | source\n", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FishConfigStartsAgent(tt.content); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPowerShellProfileStartsAgent(t *testing.T) {
	tests := []struct {
		name, content string
		want          bool
	}{
		{"empty", "", false},
		{"our snippet", PowerShellSnippet, true},
		{"iex alias", "sshush | iex\n", true},
		{"commented out", "# sshush start --shell powershell | Invoke-Expression\n", false},
		{"unrelated mention", "Set-Alias s sshush\n", false},
		{"unrelated Invoke-Expression", "starship init powershell | Invoke-Expression\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PowerShellProfileStartsAgent(tt.content); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
