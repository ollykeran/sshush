package platform

import "testing"

func TestWindowsServiceRunning(t *testing.T) {
	// The RPC endpoint mapper is running on any Windows machine that is up:
	// the service manager itself is reached through it.
	if !WindowsServiceRunning("RpcEptMapper") {
		t.Error("WindowsServiceRunning(RpcEptMapper) = false, want true")
	}
	if WindowsServiceRunning("sshush-test-no-such-service") {
		t.Error("WindowsServiceRunning of a service that does not exist = true")
	}
}
