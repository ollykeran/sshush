package sshushd

// detachProcess does nothing on Windows: a process cannot leave its console after
// the fact, so the parent starts the daemon detached instead (see detachedStart).
func detachProcess() error { return nil }
