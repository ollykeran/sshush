package transport

import (
	"fmt"
	"net"
	"strings"
	"time"

	winio "github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

const pipePrefix = `\\.\pipe\`

// dialTimeout bounds the wait for a free pipe instance. A pipe nobody is serving
// fails at once; this only matters when every instance is momentarily busy.
const dialTimeout = 2 * time.Second

// pipePath reports whether path names a pipe, and returns it with backslashes,
// so a path written with forward slashes (//./pipe/name) works too.
func pipePath(path string) (string, bool) {
	p := strings.ReplaceAll(path, "/", `\`)
	if len(p) <= len(pipePrefix) || !strings.EqualFold(p[:len(pipePrefix)], pipePrefix) {
		return "", false
	}
	return pipePrefix + p[len(pipePrefix):], true
}

// dialPipe connects to pipe and, before the caller can write a byte to it,
// checks that the far end is ours. The server's ACL (see listenPipe) only
// protects a pipe we created: the name is machine-wide and belongs to whoever
// creates it first, so another account that gets there before the agent would
// otherwise be sent the vault passphrase and every private key added.
func dialPipe(pipe string) (net.Conn, error) {
	timeout := dialTimeout
	conn, err := winio.DialPipe(pipe, &timeout)
	if err != nil {
		return nil, err
	}
	if err := checkPipeOwner(conn, pipe); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// checkPipeOwner returns an error unless the pipe conn is connected to is owned
// by an account trustedPipeOwner accepts.
//
// The owner is read from the handle that is already connected, not by opening
// the name a second time, so there is no gap in which the pipe could be swapped
// for another. It cannot be forged by the server either: Windows lets a process
// name as owner only its own user or a group its token may assign (in practice
// Administrators, for an elevated process), unless it holds a privilege that
// already lets it read everyone's files. The alternative, asking for the server's
// process id and opening its token, needs access to that process, which is
// exactly what we would not have to a stranger's.
func checkPipeOwner(conn net.Conn, pipe string) error {
	// go-winio does not export its connection type, only this method on it. If
	// that ever changes the check cannot be made, and the answer has to be no.
	f, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return fmt.Errorf("transport: cannot tell who serves %s: no handle to ask", pipe)
	}
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_KERNEL_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("transport: cannot tell who serves %s: %w", pipe, err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("transport: cannot tell who serves %s: %w", pipe, err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("transport: current user: %w", err)
	}
	if !trustedPipeOwner(owner, user.User.Sid) {
		return fmt.Errorf("%w: %s belongs to %s", ErrForeignPipe, pipe, accountName(owner))
	}
	return nil
}

// trustedPipeOwner reports whether a pipe owned by owner may be sent user's
// secrets. That is user itself, and two owners that are not user but could read
// everything of user's regardless: SYSTEM, which a service runs as (the Windows
// OpenSSH agent's pipe is SYSTEM's), and the Administrators group, which is the
// owner Windows gives objects created by an elevated process, such as a daemon
// started from an administrator prompt.
func trustedPipeOwner(owner, user *windows.SID) bool {
	if owner == nil || user == nil {
		return false
	}
	return owner.Equals(user) ||
		owner.IsWellKnown(windows.WinLocalSystemSid) ||
		owner.IsWellKnown(windows.WinBuiltinAdministratorsSid)
}

// accountName describes sid for an error message: the account's name where
// Windows knows one, and always the SID, which is what identifies it.
func accountName(sid *windows.SID) string {
	if sid == nil {
		return "nobody (the pipe has no owner)"
	}
	account, domain, _, err := sid.LookupAccount("")
	if err != nil || account == "" {
		return sid.String()
	}
	if domain != "" {
		account = domain + `\` + account
	}
	return account + " (" + sid.String() + ")"
}

// listenPipe serves pipe to the current user only. A pipe's default ACL also
// lets Everyone open it for reading, and an agent's whole job is to sign for
// whoever connects, so the ACL is spelled out: this user and SYSTEM, no one else.
func listenPipe(pipe string) (net.Listener, error) {
	sddl, err := ownerOnlySDDL()
	if err != nil {
		return nil, err
	}
	return winio.ListenPipe(pipe, &winio.PipeConfig{SecurityDescriptor: sddl})
}

// ownerOnlySDDL returns a protected DACL granting full access to the current
// user and to SYSTEM.
func ownerOnlySDDL() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("transport: current user: %w", err)
	}
	return "D:P(A;;GA;;;" + user.User.Sid.String() + ")(A;;GA;;;SY)", nil
}
