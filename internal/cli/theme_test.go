package cli

import (
	"testing"

	"github.com/ollykeran/sshush/internal/theme"
)

func TestPresetNameForTheme_knownPreset(t *testing.T) {
	t.Parallel()
	for name, th := range theme.Presets {
		got := presetNameForTheme(th)
		if got != name {
			t.Errorf("presetNameForTheme(%s) = %q, want %q", name, got, name)
		}
	}
}

func TestPresetNameForTheme_unknownTheme(t *testing.T) {
	t.Parallel()
	th := theme.Theme{Text: "#000000", Focus: "#111111", Accent: "#222222", Error: "#333333", Warning: "#444444"}
	got := presetNameForTheme(th)
	if got != "" {
		t.Errorf("presetNameForTheme(unknown) = %q, want empty", got)
	}
}

func TestThemeEqual_identical(t *testing.T) {
	t.Parallel()
	th := theme.Theme{Text: "#aaa", Focus: "#bbb", Accent: "#ccc", Error: "#ddd", Warning: "#eee"}
	if !themeEqual(th, th) {
		t.Error("themeEqual should return true for identical themes")
	}
}

func TestThemeEqual_different(t *testing.T) {
	t.Parallel()
	a := theme.Theme{Text: "#aaa", Focus: "#bbb", Accent: "#ccc", Error: "#ddd", Warning: "#eee"}
	b := theme.Theme{Text: "#aaa", Focus: "#bbb", Accent: "#ccc", Error: "#ddd", Warning: "#fff"}
	if themeEqual(a, b) {
		t.Error("themeEqual should return false for different themes")
	}
}

func TestThemeList_succeeds(t *testing.T) {
	t.Parallel()
	cmd := newThemeListCommand()
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("theme list: %v", err)
	}
}

func TestThemeShow_succeeds(t *testing.T) {
	t.Parallel()
	cmd := newThemeShowCommand()
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("theme show: %v", err)
	}
}

func TestThemeSet_invalidHex(t *testing.T) {
	t.Parallel()
	cmd := newThemeSetCommand()
	cmd.SetArgs([]string{"--accent", "not-a-hex"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestThemeSet_noArgsNoFlags(t *testing.T) {
	t.Parallel()
	cmd := newThemeSetCommand()
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no preset or flags provided")
	}
}

func TestThemeSet_unknownPreset(t *testing.T) {
	t.Parallel()
	cmd := newThemeSetCommand()
	cmd.SetArgs([]string{"nonexistent-preset-xyz"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for unknown preset")
	}
}
