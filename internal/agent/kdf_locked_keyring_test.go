package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	sshagent "golang.org/x/crypto/ssh/agent"
)

func TestKDFLockedKeyring_lockBlocksSignAndList(t *testing.T) {
	inner := sshagent.NewKeyring().(sshagent.ExtendedAgent)
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := inner.Add(sshagent.AddedKey{PrivateKey: priv, Comment: "k"}); err != nil {
		t.Fatal(err)
	}
	k := NewKDFLockedKeyring(inner)

	keys, err := k.List()
	if err != nil || len(keys) != 1 {
		t.Fatalf("list before lock: keys=%v err=%v", keys, err)
	}
	k0 := keys[0]

	if err := k.Lock([]byte("secret-lock")); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	keys, err = k.List()
	if err != nil || keys != nil {
		t.Fatalf("list when locked want empty: keys=%v err=%v", keys, err)
	}
	_, err = k.Sign(k0, []byte("data"))
	if err == nil {
		t.Fatal("Sign when locked: want error")
	}
	if !errors.Is(err, errKDFAgentLocked) {
		t.Fatalf("Sign error: %v", err)
	}

	if err := k.Unlock([]byte("wrong")); err == nil {
		t.Fatal("Unlock wrong passphrase: want error")
	}
	if err := k.Unlock([]byte("secret-lock")); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	keys, err = k.List()
	if err != nil || len(keys) != 1 {
		t.Fatalf("list after unlock: keys=%v err=%v", keys, err)
	}
}

func TestKDFLockedKeyring_doubleLock(t *testing.T) {
	inner := sshagent.NewKeyring().(sshagent.ExtendedAgent)
	k := NewKDFLockedKeyring(inner)
	if err := k.Lock([]byte("a")); err != nil {
		t.Fatal(err)
	}
	err := k.Lock([]byte("b"))
	if err == nil {
		t.Fatal("second Lock: want agent: locked")
	}
	if !errors.Is(err, errKDFAgentLocked) {
		t.Fatalf("got %v", err)
	}
}

func TestKDFLockedKeyring_unlockWhenNotLocked(t *testing.T) {
	inner := sshagent.NewKeyring().(sshagent.ExtendedAgent)
	k := NewKDFLockedKeyring(inner)
	err := k.Unlock([]byte("x"))
	if err == nil {
		t.Fatal("want not locked")
	}
	if !errors.Is(err, errKDFAgentNotLocked) {
		t.Fatalf("got %v", err)
	}
}
