package platform

import "golang.org/x/sys/windows/registry"

// PersistedEnv returns the value new processes will be given for the
// environment variable name: the user's own setting if there is one, otherwise
// the machine-wide one. That can differ from what this process has, because a
// process keeps the environment it started with — setting a variable with
// setx, or in System Properties, changes nothing in windows already open.
func PersistedEnv(name string) (string, bool) {
	for _, source := range []struct {
		root registry.Key
		path string
	}{
		{registry.CURRENT_USER, `Environment`},
		{registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`},
	} {
		key, err := registry.OpenKey(source.root, source.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		value, _, err := key.GetStringValue(name)
		_ = key.Close()
		if err == nil && value != "" {
			return value, true
		}
	}
	return "", false
}
