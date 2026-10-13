//go:build unix

package keys

import "syscall"

// The tests here check the modes of files sshush writes, and a mode asked for
// is cut down by the umask of whoever runs the tests: under 077 a public key
// written 0644 comes out 0600. Pinning it makes the modes checked the ones the
// code asked for. Windows has no umask, and its tests do not check modes.
func init() {
	syscall.Umask(0o022)
}
