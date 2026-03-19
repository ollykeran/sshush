package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	sshagent "golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh"
)

func TestAgentAuth_Authorized(t *testing.T) {
	_, priv1, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer1, err := ssh.NewSignerFromKey(priv1)
	if err != nil {
		t.Fatal(err)
	}
	pub1 := signer1.PublicKey()

	_, priv2, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer2, err := ssh.NewSignerFromKey(priv2)
	if err != nil {
		t.Fatal(err)
	}
	pub2 := signer2.PublicKey()

	keyring := sshagent.NewKeyring()
	if err := keyring.Add(sshagent.AddedKey{PrivateKey: priv1}); err != nil {
		t.Fatal(err)
	}

	auth := &AgentAuth{Agent: keyring}

	if !auth.Authorized(pub1) {
		t.Error("expected Authorized(pub1) = true (key in agent)")
	}
	if auth.Authorized(pub2) {
		t.Error("expected Authorized(pub2) = false (key not in agent)")
	}
}
