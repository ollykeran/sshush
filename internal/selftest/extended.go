package selftest

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ollykeran/sshush/internal/server"
	"github.com/ollykeran/sshush/internal/transport"
	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// extendedTimeout bounds each wait in Extended: the server coming up, and the
// SSH login itself.
const extendedTimeout = 5 * time.Second

// Extended proves the agent at socketPath can complete a real SSH login. It
// starts a temporary sshush server on loopback that authorizes the keys the agent
// lists, logs in to it with the agent's first key, and shuts the server down.
// Only the handshake and public-key authentication are exercised: no session is
// opened, which the server refuses on Windows anyway.
func Extended(socketPath string) Check {
	fail := func(format string, args ...any) Check {
		return Check{Name: "ssh", Detail: "✗ " + fmt.Sprintf(format, args...), Status: Fail,
			Hint: "the agent could not complete an SSH login"}
	}

	dir, err := os.MkdirTemp("", "sshush-selftest-")
	if err != nil {
		return fail("temp dir: %v", err)
	}
	defer os.RemoveAll(dir)
	hostKeyPath := filepath.Join(dir, "host_key")
	if _, err := server.EnsureHostKey(hostKeyPath); err != nil {
		return fail("host key: %v", err)
	}
	hostKey, err := publicHostKey(hostKeyPath)
	if err != nil {
		return fail("host key: %v", err)
	}

	ready := make(chan struct{})
	served := make(chan error, 1)
	srv := &server.Server{
		ListenAddr:  "127.0.0.1:0",
		AuthKeys:    &server.SocketAuth{SocketPath: socketPath},
		HostKeyPath: hostKeyPath,
		Ready:       func() { close(ready) },
	}
	go func() { served <- srv.ListenAndServe() }()
	defer func() {
		_ = srv.Close()
		<-served
	}()
	select {
	case <-ready:
	case err := <-served:
		served <- err // for the deferred wait
		return fail("start server: %v", err)
	case <-time.After(extendedTimeout):
		return fail("server did not start within %s", extendedTimeout)
	}
	addr := srv.Addr().String()

	conn, err := transport.Dial(socketPath)
	if err != nil {
		return fail("agent: %v", err)
	}
	defer conn.Close()
	agentClient := sshagent.NewClient(conn)
	keys, err := agentClient.List()
	if err != nil {
		return fail("agent: list: %v", err)
	}
	signers, err := agentClient.Signers()
	if err != nil {
		return fail("agent: %v", err)
	}
	if len(keys) == 0 || len(signers) == 0 {
		return fail("no keys loaded")
	}

	// One key, the same one the sign check uses, so the line can say which key
	// logged in; offering every key would leave that to whichever the server took.
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            "sshush-selftest",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signers[0])},
		HostKeyCallback: ssh.FixedHostKey(hostKey),
		Timeout:         extendedTimeout,
	})
	if err != nil {
		return fail("login to %s: %v", addr, err)
	}
	_ = client.Close()
	return Check{Name: "ssh", Detail: fmt.Sprintf("logged in to %s with %s  ✓", addr, keys[0].Comment), Status: OK}
}

// publicHostKey reads the host key at path for the client to pin, so the test
// login checks the server it started rather than trusting any host key.
func publicHostKey(path string) (ssh.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, err
	}
	return signer.PublicKey(), nil
}
