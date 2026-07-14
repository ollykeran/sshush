package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerate_customPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outPath := filepath.Join(dir, "config.toml")

	cmd := newGenerateConfigCommand()
	cmd.SetArgs([]string{outPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("generate config: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("config not created: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("config file is empty")
	}
}

func TestGenerate_noOverwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outPath := filepath.Join(dir, "config.toml")
	os.WriteFile(outPath, []byte("existing"), 0o644)

	cmd := newGenerateConfigCommand()
	cmd.SetArgs([]string{outPath})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when file exists without --force")
	}

	data, _ := os.ReadFile(outPath)
	if string(data) != "existing" {
		t.Fatal("existing file was overwritten without --force")
	}
}

func TestGenerate_force(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outPath := filepath.Join(dir, "config.toml")
	os.WriteFile(outPath, []byte("old"), 0o644)

	cmd := newGenerateConfigCommand()
	cmd.SetArgs([]string{outPath, "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("generate config --force: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "old" {
		t.Fatal("file was not overwritten with --force")
	}
}

func TestGenerate_parentDirCreated(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outPath := filepath.Join(dir, "subdir", "config.toml")

	cmd := newGenerateConfigCommand()
	cmd.SetArgs([]string{outPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("generate config with subdirs: %v", err)
	}

	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("config not created in nested dir: %v", err)
	}
}

func TestGenerate_rejectsTooManyArgs(t *testing.T) {
	t.Parallel()
	cmd := newGenerateConfigCommand()
	cmd.SetArgs([]string{"a.toml", "b.toml"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for two positional args")
	}
}
