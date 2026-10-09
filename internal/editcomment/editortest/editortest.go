// Package editortest builds stand-in editors for tests: small scripts that do
// to the file they are handed what a person in a real editor might.
package editortest

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// Script writes an editor to dir and returns its path. Run on a file, it
// replaces the file's content with lines (given none, it leaves the file
// alone, as someone quitting without saving would) and exits with exitCode.
// It is a shell script, or a .cmd batch file on Windows.
func Script(t *testing.T, dir string, exitCode int, lines ...string) string {
	t.Helper()
	var b strings.Builder
	name := "editor.sh"
	if runtime.GOOS == "windows" {
		name = "editor.cmd"
		b.WriteString("@echo off\r\n")
		for i, line := range lines {
			redirect := ">"
			if i > 0 {
				redirect = ">>"
			}
			// "echo(" prints the text as it is, even when it is only spaces.
			b.WriteString("echo(" + line + redirect + " %1\r\n")
		}
		b.WriteString("exit /b " + strconv.Itoa(exitCode) + "\r\n")
	} else {
		b.WriteString("#!/bin/sh\n")
		for i, line := range lines {
			format, redirect := "%s", ">"
			if i > 0 {
				format, redirect = `\n%s`, ">>"
			}
			b.WriteString("printf '" + format + "' '" + line + "' " + redirect + " \"$1\"\n")
		}
		b.WriteString("exit " + strconv.Itoa(exitCode) + "\n")
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(b.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
