package agent

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"

	"github.com/ollykeran/sshush/internal/style"
	"golang.org/x/crypto/ssh/agent"
)

var ErrAlreadyRunning = errors.New("agent already running on socket")

// errStyled wraps an error with a styled message for display; Unwrap() preserves errors.Is.
type errStyled struct {
	err    error
	styled string
}

func (e *errStyled) Error() string { return e.styled }
func (e *errStyled) Unwrap() error { return e.err }

func ListenAndServe(ctx context.Context, socketPath string, keyring agent.Agent) error {
	os.MkdirAll(filepath.Dir(socketPath), 0700)
	if conn, err := net.Dial("unix", socketPath); err == nil {
		conn.Close()
		return &errStyled{err: ErrAlreadyRunning, styled: style.Err(ErrAlreadyRunning.Error())}
	}
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()

	os.Setenv("SSH_AUTH_SOCK", socketPath)

	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go agent.ServeAgent(keyring, conn)
	}
}