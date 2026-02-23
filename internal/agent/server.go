package agent

import (
	"context"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh/agent"
)

func ListenAndServe(ctx context.Context, socketPath string, keyring agent.Agent) error {
	os.MkdirAll(filepath.Dir(socketPath), 0700)
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