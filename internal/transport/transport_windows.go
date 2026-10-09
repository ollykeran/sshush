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

func dialPipe(pipe string) (net.Conn, error) {
	timeout := dialTimeout
	return winio.DialPipe(pipe, &timeout)
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
