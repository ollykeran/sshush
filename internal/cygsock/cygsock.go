// Package cygsock serves the agent the way Cygwin and MSYS2 programs reach a
// Unix socket on Windows. That is the ssh in Git for Windows and Git Bash, and
// so git itself unless it is pointed at Windows' OpenSSH.
//
// Cygwin has no real Unix sockets there. Its "socket" is an ordinary file,
// marked as a system file, that reads
//
//	!<socket >PORT s XXXXXXXX-XXXXXXXX-XXXXXXXX-XXXXXXXX
//
// The client connects to 127.0.0.1:PORT and sends the 16-byte secret the file
// holds (four 32-bit words, little-endian on the wire). The server checks it
// and sends it back, then the two swap 12 bytes of credentials (pid, uid,
// gid). After that the connection carries the agent protocol as usual.
//
// The secret is what keeps other users out: any process can connect to the
// port, but only one that can read the file knows what to send. The file is
// written so that only the current user can read it.
package cygsock

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/ollykeran/sshush/internal/secfile"
)

// handshakeTimeout bounds how long a client may take over the handshake
// before it is dropped.
const handshakeTimeout = 10 * time.Second

// Secret is the 16 bytes a client must send, as they go on the wire.
type Secret [16]byte

// fileLine is the socket file's content for port and secret.
func fileLine(port int, s Secret) string {
	w := func(i int) uint32 { return binary.LittleEndian.Uint32(s[4*i:]) }
	return fmt.Sprintf("!<socket >%d s %08x-%08x-%08x-%08x", port, w(0), w(1), w(2), w(3))
}

// Serve writes the socket file at path and hands every client that passes
// the handshake to handle, until ctx is done. Then it stops listening and
// removes the file, unless something else has replaced it meanwhile.
// ready, if not nil, is called once the file is in place.
func Serve(ctx context.Context, path string, handle func(net.Conn), ready func()) error {
	var secret Secret
	if _, err := rand.Read(secret[:]); err != nil {
		return fmt.Errorf("cygwin socket: secret: %w", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("cygwin socket: listen: %w", err)
	}
	defer ln.Close()
	line := fileLine(ln.Addr().(*net.TCPAddr).Port, secret)
	if err := writeSocketFile(path, line); err != nil {
		return err
	}
	defer removeIfOurs(path, line)
	if ready != nil {
		ready()
	}

	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return fmt.Errorf("cygwin socket: accept: %w", err)
		}
		go func() {
			if err := Handshake(conn, secret); err != nil {
				conn.Close()
				return
			}
			handle(conn)
		}()
	}
}

// Handshake is the server's half of the Cygwin connection handshake on conn.
// On error the caller closes conn; nothing of the agent's is sent first.
func Handshake(conn net.Conn, secret Secret) error {
	_ = conn.SetDeadline(time.Now().Add(handshakeTimeout))
	var got Secret
	if _, err := io.ReadFull(conn, got[:]); err != nil {
		return fmt.Errorf("cygwin socket: read secret: %w", err)
	}
	if subtle.ConstantTimeCompare(got[:], secret[:]) != 1 {
		return errors.New("cygwin socket: wrong secret")
	}
	if _, err := conn.Write(secret[:]); err != nil {
		return fmt.Errorf("cygwin socket: send secret: %w", err)
	}
	// The client's pid, uid and gid. They are not checked (the secret was the
	// check); the reply carries this process's pid and the client's uid and
	// gid, which is what a Cygwin server of the same user would send.
	var cred [12]byte
	if _, err := io.ReadFull(conn, cred[:]); err != nil {
		return fmt.Errorf("cygwin socket: read credentials: %w", err)
	}
	binary.LittleEndian.PutUint32(cred[:4], uint32(os.Getpid()))
	if _, err := conn.Write(cred[:]); err != nil {
		return fmt.Errorf("cygwin socket: send credentials: %w", err)
	}
	return conn.SetDeadline(time.Time{})
}

// writeSocketFile writes line to path, readable only by the current user,
// and marks it as Cygwin marks a socket.
func writeSocketFile(path, line string) error {
	if err := secfile.MkdirAll(filepath.Dir(path)); err != nil {
		return fmt.Errorf("cygwin socket: %w", err)
	}
	// A socket file left by an agent that did not exit cleanly is replaced;
	// its attributes are cleared first, as a system file may not be rewritten
	// in place.
	clearAttributes(path)
	if err := secfile.WriteFile(path, []byte(line)); err != nil {
		return fmt.Errorf("cygwin socket: write %s: %w", path, err)
	}
	if err := markSocket(path); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("cygwin socket: mark %s: %w", path, err)
	}
	return nil
}

// removeIfOurs removes the socket file if it still says line: another agent
// that has since written its own is left alone.
func removeIfOurs(path, line string) {
	data, err := os.ReadFile(path)
	if err != nil || string(data) != line {
		return
	}
	clearAttributes(path)
	_ = os.Remove(path)
}
