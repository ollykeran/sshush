package server

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	sshagent "golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh"
)

func TestServer_ListenAndServe_sessionMessage(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}

	keyring := sshagent.NewKeyring()
	if err := keyring.Add(sshagent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	srv := &Server{
		ListenAddr:  addr,
		AuthKeys:    &AgentAuth{Agent: keyring},
		HostKeyPath: "",
	}
	go func() { _ = srv.ListenAndServe() }()
	time.Sleep(100 * time.Millisecond)

	config := &ssh.ClientConfig{
		User: "test",
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	conn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	stdout, err := session.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Shell(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	buf.ReadFrom(stdout)
	session.Close()

	scanner := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
	if !scanner.Scan() {
		t.Fatal("expected at least one line")
	}
	line := scanner.Text()
	if line != "sshush session (authorized by key)" {
		t.Errorf("session output = %q, want %q", line, "sshush session (authorized by key)")
	}
}

// TestServer_RejectUnauthorizedKey ensures a client with a key not in the auth source is rejected.
func TestServer_RejectUnauthorizedKey(t *testing.T) {
	_, privAuthorized, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signerAuthorized, err := ssh.NewSignerFromKey(privAuthorized)
	if err != nil {
		t.Fatal(err)
	}
	_, privUnknown, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signerUnknown, err := ssh.NewSignerFromKey(privUnknown)
	if err != nil {
		t.Fatal(err)
	}

	keyring := sshagent.NewKeyring()
	if err := keyring.Add(sshagent.AddedKey{PrivateKey: privAuthorized}); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	srv := &Server{
		ListenAddr:  addr,
		AuthKeys:    &AgentAuth{Agent: keyring},
		HostKeyPath: "",
	}
	go func() { _ = srv.ListenAndServe() }()
	time.Sleep(100 * time.Millisecond)

	config := &ssh.ClientConfig{
		User:            "test",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signerUnknown)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	conn, err := ssh.Dial("tcp", addr, config)
	if err == nil {
		conn.Close()
		t.Fatal("expected SSH auth to fail for unauthorized key")
	}
	if conn != nil {
		t.Fatal("conn should be nil on auth failure")
	}
	// Sanity: authorized key still works
	configOK := &ssh.ClientConfig{
		User:            "test",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signerAuthorized)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	connOK, err := ssh.Dial("tcp", addr, configOK)
	if err != nil {
		t.Fatalf("authorized key should connect: %v", err)
	}
	connOK.Close()
}

// TestServer_HostKeyFromFile runs the server with a host key from a temp file and connects.
func TestServer_HostKeyFromFile(t *testing.T) {
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, clientPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(clientPriv)
	if err != nil {
		t.Fatal(err)
	}
	keyring := sshagent.NewKeyring()
	if err := keyring.Add(sshagent.AddedKey{PrivateKey: clientPriv}); err != nil {
		t.Fatal(err)
	}

	block, err := ssh.MarshalPrivateKey(hostPriv, "")
	if err != nil {
		t.Fatal(err)
	}
	hostKeyFile := filepath.Join(t.TempDir(), "host_key")
	if err := os.WriteFile(hostKeyFile, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	srv := &Server{
		ListenAddr:  addr,
		AuthKeys:    &AgentAuth{Agent: keyring},
		HostKeyPath: hostKeyFile,
	}
	go func() { _ = srv.ListenAndServe() }()
	time.Sleep(100 * time.Millisecond)

	config := &ssh.ClientConfig{
		User:            "test",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	conn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		t.Fatalf("connect with host key file: %v", err)
	}
	conn.Close()
}
