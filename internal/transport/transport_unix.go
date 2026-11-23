//go:build !windows

package transport

import (
	"errors"
	"net"
)

var errNoPipes = errors.New("transport: named pipes are only supported on Windows")

// pipePath never matches off Windows: a path that looks like \\.\pipe\name is
// just an odd filename there.
func pipePath(string) (string, bool) { return "", false }

func dialPipe(string) (net.Conn, error) { return nil, errNoPipes }

func listenPipe(string) (net.Listener, error) { return nil, errNoPipes }
