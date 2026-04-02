package platform

import (
	"path/filepath"
	"testing"
)

func TestShellRcPathForAutoSetup_zsh(t *testing.T) {
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
