package main

import (
	"testing"
	"os"
	"path/filepath"
)

func TestExpandHomeDirectory(t *testing.T) {
	home_dir, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	
	cases := []struct {
		name string
		path string
		want string
	}{
		{"home directory", "~/id_rsa", filepath.Join(home_dir, "id_rsa")},
		{"relative path", "./id_rsa", "./id_rsa"},
		{"absolute path", filepath.Join(home_dir, "id_rsa"), filepath.Join(home_dir, "id_rsa")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExpandHomeDirectory(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// func TestExpandRelativePath(t *testing.T) {
// 	cwd, err := os.Getwd()
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	cases := []struct {
// 		name string
// 		path string
// 		want string
// 	}{
// 		{"relative path", "./id_rsa", filepath.Join(cwd, "/", "id_rsa")},
// 	}

// 	for _, tc := range cases {
// 		t.Run(tc.name, func(t *testing.T) {
// 			got, err := ExpandRelativePath(tc.path)
// 			if err != nil {
// 				t.Fatal(err)
// 			}
// 			if got != tc.want {
// 				t.Errorf("got %q, want %q", got, tc.want)
// 			}
// 		})
// 	}
// }