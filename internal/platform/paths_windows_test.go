package platform

import (
	"path/filepath"
	"testing"
)

// The config directory is ~\.config\sshush, as on Unix, and not under
// %LOCALAPPDATA% even when that is set.
func TestConfigDir_isUnderTheProfile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "AppData", "Local"))

	want := filepath.Join(tmp, ".config", "sshush")
	if got := ConfigDir(); got != want {
		t.Fatalf("ConfigDir: got %q, want %q", got, want)
	}
	// With no XDG_RUNTIME_DIR, the pidfile lives there too.
	t.Setenv("XDG_RUNTIME_DIR", "")
	if got := DefaultPidFilePath(); got != filepath.Join(want, PidFileName) {
		t.Fatalf("DefaultPidFilePath: got %q", got)
	}
}

// The agent's endpoint is a named pipe wherever the config and runtime dirs are.
func TestDefaultSocketPath_isTheNamedPipe(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if got := DefaultSocketPath(); got != `\\.\pipe\sshush-agent` {
		t.Fatalf("DefaultSocketPath: got %q", got)
	}
}

func TestShellSetupForAutoSetup_powerShellProfile(t *testing.T) {
	// A home that is not the account's own: the profile is looked for under it,
	// which is what keeps this test away from the real one.
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)

	got, ok := ShellSetupForAutoSetup()
	if !ok {
		t.Fatal("expected ok")
	}
	if !got.PowerShell || got.Fish || got.Snippet != PowerShellSnippet {
		t.Fatalf("expected the PowerShell setup, got %+v", got)
	}
	if dir := filepath.Dir(filepath.Dir(got.RcPath)); dir != filepath.Join(home, "Documents") {
		t.Fatalf("profile %q is not under %q", got.RcPath, filepath.Join(home, "Documents"))
	}
	if filepath.Base(got.RcPath) != "Microsoft.PowerShell_profile.ps1" {
		t.Fatalf("unexpected profile name in %q", got.RcPath)
	}
}

func TestPowerShellProfilePath_perEdition(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	for parent, dir := range map[string]string{
		"powershell.exe": "WindowsPowerShell",
		"pwsh.exe":       "PowerShell",
		"cmd.exe":        "WindowsPowerShell",
		"":               "WindowsPowerShell",
	} {
		got, ok := powerShellProfilePath(parent)
		if !ok || filepath.Base(filepath.Dir(got)) != dir {
			t.Errorf("parent %q: got %q, %v; want a profile under %s", parent, got, ok, dir)
		}
	}
}

func TestParentProcessName_findsTheParent(t *testing.T) {
	if parentProcessName() == "" {
		t.Fatal("expected the name of the process that started the tests")
	}
}
