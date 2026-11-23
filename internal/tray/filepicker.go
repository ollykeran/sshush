package tray

import (
	"path/filepath"
	"unicode/utf16"
)

// splitPickerResult reads the multi-select buffer: a lone full path, or the
// folder followed by each file name, NUL-separated and ending in two NULs.
func splitPickerResult(buf []uint16) []string {
	var parts []string
	start := 0
	for i, c := range buf {
		if c != 0 {
			continue
		}
		if i == start {
			break // the second NUL of the pair
		}
		parts = append(parts, string(utf16.Decode(buf[start:i])))
		start = i + 1
	}
	if len(parts) <= 1 {
		return parts
	}
	dir, names := parts[0], parts[1:]
	paths := make([]string, len(names))
	for i, n := range names {
		paths[i] = filepath.Join(dir, n)
	}
	return paths
}
