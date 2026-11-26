//go:build !windows

package platform

// PersistedEnv has no answer off Windows, where a variable set for future
// shells lives in whichever startup file the user's shell reads.
func PersistedEnv(string) (string, bool) { return "", false }

// UserEnv has no answer off Windows; see PersistedEnv.
func UserEnv(string) (string, bool) { return "", false }

// SetUserEnv does nothing off Windows, where there is no one place that every
// new program's environment comes from.
func SetUserEnv(string, string) error { return nil }
