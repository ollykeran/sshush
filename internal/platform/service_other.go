//go:build !windows

package platform

// WindowsServiceRunning is false off Windows, where there are no such services.
func WindowsServiceRunning(string) bool { return false }
