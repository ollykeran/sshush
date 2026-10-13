//go:build !windows

package platform

// PersistedEnv has no answer off Windows, where a variable set for future
// shells lives in whichever startup file the user's shell reads.
func PersistedEnv(string) (string, bool) { return "", false }
