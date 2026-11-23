package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ollykeran/sshush/internal/config"
)

func TestRunFind_noKeys(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if err := runFind(true, false, nil, dir); err != nil {
		t.Fatalf("runFind: %v", err)
	}
}

func TestRunFind_discoverKeys(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestKey(t, dir, "id_ed25519", "find-ed25519")

	if err := runFind(true, false, nil, dir); err != nil {
		t.Fatalf("runFind: %v", err)
	}
}

func TestRunFind_multipleKeyTypes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestKey(t, dir, "id_ed25519", "find-ed25519")
	if err := runCreate("rsa", 2048, "find-rsa", filepath.Join(dir, "id_rsa"), false); err != nil {
		t.Fatal(err)
	}
	if err := runCreate("ecdsa", 256, "find-ecdsa", filepath.Join(dir, "id_ecdsa"), false); err != nil {
		t.Fatal(err)
	}

	if err := runFind(true, false, nil, dir); err != nil {
		t.Fatalf("runFind multiple types: %v", err)
	}
}

func TestRunFind_recursive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0o755)
	writeTestKey(t, sub, "id_ed25519", "recursive-key")

	// Without recursive, the subdirectory key should not be found.
	if err := runFind(true, false, nil, dir); err != nil {
		t.Fatalf("runFind non-recursive: %v", err)
	}

	// With recursive, it should be found.
	if err := runFind(true, true, nil, dir); err != nil {
		t.Fatalf("runFind recursive: %v", err)
	}
}

func TestRunFind_emptyDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if err := runFind(true, false, nil, dir); err != nil {
		t.Fatalf("runFind empty: %v", err)
	}
}

func TestFindSearchDirs(t *testing.T) {
	t.Parallel()
	config := []string{"/srv/keys"}
	if got, want := findSearchDirs(false, config, []string{"/given"}), []string{"/given", "/srv/keys"}; !reflect.DeepEqual(got, want) {
		t.Errorf("with defaults: %v, want %v", got, want)
	}
	if got, want := findSearchDirs(true, config, []string{"/given"}), []string{"/given"}; !reflect.DeepEqual(got, want) {
		t.Errorf("--no-defaults: %v, want %v (search_dirs are a default)", got, want)
	}
	if got, want := findSearchDirs(false, config, nil), []string{"/srv/keys"}; !reflect.DeepEqual(got, want) {
		t.Errorf("no paths given: %v, want %v", got, want)
	}
}

func TestFindCommand_usesConfigSearchDirs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestKey(t, dir, "id_ed25519", "find-config-dir")
	missing := filepath.Join(dir, "missing")

	cmd := newFindCommand()
	withConfig(cmd, &config.Config{SearchDirs: []string{missing, dir}})
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("find with configured search_dirs: %v", err)
	}
}

func TestFindCommand_rejectsInvalidFlag(t *testing.T) {
	t.Parallel()
	cmd := newFindCommand()
	cmd.SetArgs([]string{"--bogus"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid flag")
	}
}
