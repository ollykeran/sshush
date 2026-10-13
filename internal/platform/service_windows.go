package platform

import "golang.org/x/sys/windows"

// WindowsServiceRunning reports whether the Windows service called name (its
// service name, not the display name) is running or on its way up. A service
// that does not exist, or that cannot be asked, counts as not running: this is
// for noticing something in use, and what cannot be seen is not evidence of it.
//
// The service manager and the service are opened with the rights a status
// query needs and no more. The convenient mgr.Connect asks for full control of
// the service manager, which only an administrator is given, so it would fail
// for exactly the ordinary user this is run as.
func WindowsServiceRunning(name string) bool {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return false
	}
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false
	}
	defer windows.CloseServiceHandle(manager)

	service, err := windows.OpenService(manager, namePtr, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return false
	}
	defer windows.CloseServiceHandle(service)

	var status windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(service, &status); err != nil {
		return false
	}
	return status.CurrentState == windows.SERVICE_RUNNING || status.CurrentState == windows.SERVICE_START_PENDING
}
