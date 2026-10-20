package tray

import (
	"path/filepath"
	"slices"
	"testing"
	"unicode/utf16"
)

func pickerBuffer(parts ...string) []uint16 {
	var b []uint16
	for _, p := range parts {
		b = append(append(b, utf16.Encode([]rune(p))...), 0)
	}
	return append(b, 0, 0, 0) // the closing NUL, then unused buffer
}

func TestSplitPickerResultOneFile(t *testing.T) {
	path := filepath.Join("C:", "Users", "o", ".ssh", "id_ed25519")
	if got := splitPickerResult(pickerBuffer(path)); !slices.Equal(got, []string{path}) {
		t.Errorf("got %q", got)
	}
}

func TestSplitPickerResultManyFiles(t *testing.T) {
	dir := filepath.Join("C:", "Users", "o", ".ssh")
	got := splitPickerResult(pickerBuffer(dir, "id_ed25519", "id_rsa"))
	want := []string{filepath.Join(dir, "id_ed25519"), filepath.Join(dir, "id_rsa")}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSplitPickerResultEmpty(t *testing.T) {
	if got := splitPickerResult(make([]uint16, 8)); len(got) != 0 {
		t.Errorf("got %q", got)
	}
}
