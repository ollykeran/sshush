package platform

import "testing"

func TestPersistedEnv(t *testing.T) {
	// Every Windows machine has a machine-wide Path.
	if v, ok := PersistedEnv("Path"); !ok || v == "" {
		t.Errorf("PersistedEnv(Path) = %q, %v; want a value", v, ok)
	}
	if v, ok := PersistedEnv("SSHUSH_TEST_NO_SUCH_VARIABLE"); ok {
		t.Errorf("PersistedEnv of an unset variable = %q, true", v)
	}
}
