package utils

import (
	"os"
	"strings"
)

// ExpandHomeDirectory replaces ~ with the current user's home directory.
func ExpandHomeDirectory(path string) string {
	if strings.Contains(path, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return strings.ReplaceAll(path, "~", homeDir)
	}
	return path
}
