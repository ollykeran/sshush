package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandHomeDirectory(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		path string
		want string
	}{
		{"home directory", "~/id_rsa", filepath.Join(homeDir, "id_rsa")},
		{"relative path", "./id_rsa", "./id_rsa"},
		{"absolute path", filepath.Join(homeDir, "id_rsa"), filepath.Join(homeDir, "id_rsa")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExpandHomeDirectory(tc.path)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestContractHomeDirectory(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		path string
		want string
	}{
		{"home directory", "~/id_rsa", "~/id_rsa"},
		{"relative path", "./id_rsa", "./id_rsa"},
		{"absolute path", filepath.Join(homeDir, "id_rsa"), "~/id_rsa"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ContractHomeDirectory(tc.path)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDisplayPath(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	absUnderHome := filepath.Join(homeDir, "foo", "bar")
	got := DisplayPath(absUnderHome)
	want := filepath.Join("~", "foo", "bar")
	if got != want {
		t.Errorf("under home: got %q, want %q", got, want)
	}
	if got := DisplayPath(""); got != "" {
		t.Errorf("empty: got %q", got)
	}
}
