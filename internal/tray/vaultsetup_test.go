package tray

import (
	"strings"
	"testing"
)

func TestNewPassphraseProblem(t *testing.T) {
	tests := []struct {
		pass, confirm string
		nilConfirm    bool
		want          string
	}{
		{"short", "", true, "at least 8"},
		{"long enough", "", true, ""},
		{"long enough", "long enough", false, ""},
		{"long enough", "different!!", false, "did not match"},
		{"ünïcödé", "", true, "at least 8"}, // 7 characters, more bytes
	}
	for _, tc := range tests {
		var confirm []byte
		if !tc.nilConfirm {
			confirm = []byte(tc.confirm)
		}
		got := newPassphraseProblem([]byte(tc.pass), confirm)
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("%q/%q: got %q, want %q", tc.pass, tc.confirm, got, tc.want)
		}
	}
}

func TestPhraseLines(t *testing.T) {
	words := strings.Fields(strings.Repeat("abandon ", 23) + "art")
	got := phraseLines(strings.Join(words, " "))
	lines := strings.Split(got, "\r\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4 of six words:\n%s", len(lines), got)
	}
	if !strings.HasPrefix(lines[0], "1. abandon") || !strings.HasSuffix(lines[3], "24. art") {
		t.Errorf("numbering: got\n%s", got)
	}
}

func TestRecoveryMessageNamesTheFile(t *testing.T) {
	if got := recoveryMessage(""); strings.Contains(got, "saved to") {
		t.Errorf("no file: got %q", got)
	}
	if got := recoveryMessage("/v/recovery.txt"); !strings.Contains(got, "recovery.txt") {
		t.Errorf("with file: got %q", got)
	}
}
