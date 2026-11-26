//go:build !windows

package cygsock

// There are no file attributes to set off Windows; the package still works
// there, which lets its tests run on every OS.
func markSocket(string) error { return nil }

func clearAttributes(string) {}
