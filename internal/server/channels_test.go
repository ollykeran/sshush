package server

import (
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// refusedWithin runs fn, which asks the server for something it does not serve,
// failing the test if no answer comes back promptly: a refusal that arrives as a
// hang is the failure these tests exist to catch.
func refusedWithin(t *testing.T, what string, fn func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("%s was allowed, want it refused", what)
		}
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("%s hung instead of being refused", what)
		return nil
	}
}

func TestServer_RefusesLocalPortForwardingByName(t *testing.T) {
	addr, signer := startShellServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()

	err := refusedWithin(t, "ssh -L", func() error {
		forwarded, err := conn.Dial("tcp", "127.0.0.1:9")
		if err == nil {
			forwarded.Close()
		}
		return err
	})
	var open *ssh.OpenChannelError
	if !errors.As(err, &open) {
		t.Fatalf("error = %v, want an *ssh.OpenChannelError", err)
	}
	if open.Reason != ssh.Prohibited || !strings.Contains(open.Message, "does not support port forwarding") {
		t.Errorf("refusal = %v %q, want administratively prohibited, naming port forwarding", open.Reason, open.Message)
	}
}

func TestServer_RefusesRemotePortForwarding(t *testing.T) {
	addr, signer := startShellServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()

	refusedWithin(t, "ssh -R", func() error {
		listener, err := conn.Listen("tcp", "127.0.0.1:0")
		if err == nil {
			listener.Close()
		}
		return err
	})
}

// TestServer_RefusesTheSftpSubsystem covers sftp, and with it plain scp, which
// runs over sftp in current OpenSSH.
func TestServer_RefusesTheSftpSubsystem(t *testing.T) {
	addr, signer := startShellServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()

	sess, err := conn.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer sess.Close()
	refusedWithin(t, "the sftp subsystem", func() error { return sess.RequestSubsystem("sftp") })
}

func TestServer_RefusesUnknownChannelTypesByName(t *testing.T) {
	addr, signer := startShellServer(t)
	conn := dialShellServer(t, addr, signer)
	defer conn.Close()

	const channelType = "sshush-test@example.com"
	err := refusedWithin(t, "an unknown channel type", func() error {
		ch, requests, err := conn.OpenChannel(channelType, nil)
		if err == nil {
			go ssh.DiscardRequests(requests)
			ch.Close()
		}
		return err
	})
	var open *ssh.OpenChannelError
	if !errors.As(err, &open) {
		t.Fatalf("error = %v, want an *ssh.OpenChannelError", err)
	}
	if open.Reason != ssh.UnknownChannelType || !strings.Contains(open.Message, channelType) {
		t.Errorf("refusal = %v %q, want unknown channel type, naming %s", open.Reason, open.Message, channelType)
	}
}
