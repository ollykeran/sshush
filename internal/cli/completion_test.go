package cli

import (
	"testing"
)

func TestCompletion_rejectsInvalidShell(t *testing.T) {
	t.Parallel()
	cmd := newCompletionCommand()
	cmd.SetArgs([]string{"invalid"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid shell")
	}
}

func TestCompletion_rejectsNoArgs(t *testing.T) {
	t.Parallel()
	cmd := newCompletionCommand()
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for no args")
	}
}

func TestCompletion_bash(t *testing.T) {
	t.Parallel()
	cmd := newCompletionCommand()
	cmd.SetArgs([]string{"bash"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("completion bash: %v", err)
	}
}

func TestCompletion_zsh(t *testing.T) {
	t.Parallel()
	cmd := newCompletionCommand()
	cmd.SetArgs([]string{"zsh"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("completion zsh: %v", err)
	}
}

func TestCompletion_fish(t *testing.T) {
	t.Parallel()
	cmd := newCompletionCommand()
	cmd.SetArgs([]string{"fish"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("completion fish: %v", err)
	}
}
