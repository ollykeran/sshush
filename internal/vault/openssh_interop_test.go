package vault

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/ollykeran/sshush/internal/agent"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// newUnlockedVaultAgent returns an unlocked VaultAgent backed by a fresh store.
func newUnlockedVaultAgent(t *testing.T) (*VaultAgent, *VaultStore, []byte) {
	t.Helper()
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "vault.json"))
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("openssh-interop-test")
	if err := Init(store, passphrase); err != nil {
		t.Fatal(err)
	}
	va := NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}
	return va, store, passphrase
}

// TestVaultAgent_LockUnlockState verifies Lock/Unlock reply rules match OpenSSH:
// Lock fails if already locked; Unlock fails if not locked or wrong passphrase.
func TestVaultAgent_LockUnlockState(t *testing.T) {
	va, _, passphrase := newUnlockedVaultAgent(t)

	if err := va.Unlock(passphrase); err != errAgentNotLocked {
		t.Errorf("Unlock while already unlocked: want errAgentNotLocked, got %v", err)
	}

	if err := va.Lock(nil); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if err := va.Lock(nil); err != errAgentLocked {
		t.Errorf("Lock while already locked: want errAgentLocked, got %v", err)
	}
	if err := va.Unlock([]byte("wrong-passphrase")); err != errWrongPassphrase {
		t.Errorf("Unlock with wrong passphrase: want errWrongPassphrase, got %v", err)
	}
	if err := va.Unlock(passphrase); err != nil {
		t.Fatalf("Unlock with correct passphrase: %v", err)
	}

	if err := va.Lock(nil); err != nil {
		t.Fatalf("re-Lock: %v", err)
	}
	if err := va.Unlock(passphrase); err != nil {
		t.Fatalf("final Unlock: %v", err)
	}
}

// TestVaultAgent_RemoveMissingKey verifies removing an unknown key fails like OpenSSH.
func TestVaultAgent_RemoveMissingKey(t *testing.T) {
	va, _, _ := newUnlockedVaultAgent(t)

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := va.Remove(signer.PublicKey()); err != errKeyNotFound {
		t.Errorf("Remove missing key: want errKeyNotFound, got %v", err)
	}
}

// TestVaultAgent_SignWithFlags_RSA verifies rsa-sha2-256/512 signing like OpenSSH.
func TestVaultAgent_SignWithFlags_RSA(t *testing.T) {
	va, _, _ := newUnlockedVaultAgent(t)

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if err := va.Add(sshagent.AddedKey{PrivateKey: priv, Comment: "rsa-sha2"}); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pub := signer.PublicKey()
	data := []byte("sign me with rsa-sha2")

	for _, tc := range []struct {
		flags sshagent.SignatureFlags
		algo  string
	}{
		{sshagent.SignatureFlagRsaSha256, ssh.KeyAlgoRSASHA256},
		{sshagent.SignatureFlagRsaSha512, ssh.KeyAlgoRSASHA512},
	} {
		sig, err := va.SignWithFlags(pub, data, tc.flags)
		if err != nil {
			t.Fatalf("SignWithFlags(%v): %v", tc.flags, err)
		}
		if sig.Format != tc.algo {
			t.Errorf("signature format: want %q, got %q", tc.algo, sig.Format)
		}
		if err := pub.Verify(data, sig); err != nil {
			t.Errorf("verify %v: %v", tc.algo, err)
		}
	}

	sig, err := va.SignWithFlags(pub, data, 0)
	if err != nil {
		t.Fatalf("SignWithFlags(0): %v", err)
	}
	if sig.Format != ssh.KeyAlgoRSA {
		t.Errorf("flags 0: want %q, got %q", ssh.KeyAlgoRSA, sig.Format)
	}
}

// TestVaultAgent_SignWithFlags_NonRSA verifies ed25519 rejects rsa-sha2 flags,
// and locked vault rejects flags too.
func TestVaultAgent_SignWithFlags_NonRSA(t *testing.T) {
	va, _, _ := newUnlockedVaultAgent(t)

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := va.Add(sshagent.AddedKey{PrivateKey: priv, Comment: "ed"}); err != nil {
		t.Fatal(err)
	}
	signer, _ := ssh.NewSignerFromKey(priv)
	pub := signer.PublicKey()

	if _, err := va.SignWithFlags(pub, []byte("x"), sshagent.SignatureFlagRsaSha256); err == nil {
		t.Error("ed25519 with rsa-sha2 flags: want error, got nil")
	}

	if err := va.Lock(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := va.SignWithFlags(pub, []byte("x"), sshagent.SignatureFlagRsaSha256); err != errLocked {
		t.Errorf("locked SignWithFlags: want errLocked, got %v", err)
	}
}

// TestExtension_Query verifies the OpenSSH-style "query" extension returns the
// vault extension names as a list of SSH strings.
func TestExtension_Query(t *testing.T) {
	va, _, _ := newUnlockedVaultAgent(t)

	resp, err := va.Extension(ExtensionQuery, nil)
	if err != nil {
		t.Fatalf("query extension: %v", err)
	}

	var names []string
	rest := resp
	for len(rest) > 0 {
		var s struct {
			Name string
			Rest []byte `ssh:"rest"`
		}
		if err := ssh.Unmarshal(rest, &s); err != nil {
			t.Fatalf("parse extension name: %v", err)
		}
		names = append(names, s.Name)
		rest = s.Rest
	}

	want := []string{
		ExtensionQuery,
		ExtensionVaultLocked,
		ExtensionUnlockRecovery,
		ExtensionAddKeyOpts,
		ExtensionVaultSessionLoad,
		ExtensionVaultSessionUnload,
		ExtensionVaultSetAutoload,
		ExtensionVaultSetComment,
	}
	if len(names) != len(want) {
		t.Fatalf("query names: want %v, got %v", want, names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("query name[%d]: want %q, got %q", i, want[i], names[i])
		}
	}

	if _, err := va.Extension("no-such-extension", nil); err != sshagent.ErrExtensionUnsupported {
		t.Errorf("unknown extension: want ErrExtensionUnsupported, got %v", err)
	}
}

// TestVaultAgent_AddRejectsUnsupportedConstraints verifies Add fails closed on
// confirm and extension constraints and on certificates.
func TestVaultAgent_AddRejectsUnsupportedConstraints(t *testing.T) {
	va, _, _ := newUnlockedVaultAgent(t)

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := va.Add(sshagent.AddedKey{PrivateKey: priv, ConfirmBeforeUse: true}); err == nil {
		t.Error("Add with confirm constraint: want error, got nil")
	}
	if err := va.Add(sshagent.AddedKey{PrivateKey: priv, ConstraintExtensions: []sshagent.ConstraintExtension{{ExtensionName: "foo@example.com"}}}); err == nil {
		t.Error("Add with constraint extension: want error, got nil")
	}
	if err := va.Add(sshagent.AddedKey{PrivateKey: priv, Certificate: &ssh.Certificate{}}); err == nil {
		t.Error("Add with certificate: want error, got nil")
	}

	keys, err := va.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Errorf("after rejected Adds: want 0 keys, got %d", len(keys))
	}
}

// TestVaultAgent_Lifetime verifies keys added with a lifetime expire and that a
// re-add replaces the lifetime.
func TestVaultAgent_Lifetime(t *testing.T) {
	va, _, _ := newUnlockedVaultAgent(t)

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, _ := ssh.NewSignerFromKey(priv)
	if err := va.Add(sshagent.AddedKey{PrivateKey: priv, Comment: "expiring", LifetimeSecs: 1}); err != nil {
		t.Fatal(err)
	}

	keys, err := va.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("immediately after add: want 1 key, got %d", len(keys))
	}

	time.Sleep(1100 * time.Millisecond)

	keys, err = va.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Errorf("after expiry: want 0 keys, got %d", len(keys))
	}
	if _, err := va.Sign(signer.PublicKey(), []byte("x")); err != errKeyNotFound {
		t.Errorf("Sign expired key: want errKeyNotFound, got %v", err)
	}

	if err := va.Add(sshagent.AddedKey{PrivateKey: priv, Comment: "expiring", LifetimeSecs: 3600}); err != nil {
		t.Fatal(err)
	}
	keys, err = va.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Errorf("after re-add: want 1 key, got %d", len(keys))
	}
	if _, err := va.Sign(signer.PublicKey(), []byte("x")); err != nil {
		t.Errorf("Sign re-added key: %v", err)
	}
}

// TestVaultAgent_ServeAgent_OpenSSHInterop verifies OpenSSH-shaped behavior over
// the wire through ServeAgent + agent client: rsa-sha2 signing, lock/unlock state,
// remove-missing failure, query extension, and constraint rejection.
func TestVaultAgent_ServeAgent_OpenSSHInterop(t *testing.T) {
	va, _, passphrase := newUnlockedVaultAgent(t)

	sockDir := unixSocketTempDir(t)
	socketPath := filepath.Join(sockDir, "agent.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = agent.ListenAndServe(ctx, socketPath, va)
	}()

	var conn net.Conn
	var err error
	for i := 0; i < 50; i++ {
		conn, err = net.Dial("unix", socketPath)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	client := sshagent.NewClient(conn)

	// Add an RSA key and sign with rsa-sha2 flags.
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Add(sshagent.AddedKey{PrivateKey: priv, Comment: "wire-rsa"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	signer, _ := ssh.NewSignerFromKey(priv)
	pub := signer.PublicKey()
	data := []byte("wire data")
	sig, err := client.SignWithFlags(pub, data, sshagent.SignatureFlagRsaSha256)
	if err != nil {
		t.Fatalf("SignWithFlags: %v", err)
	}
	if sig.Format != ssh.KeyAlgoRSASHA256 {
		t.Errorf("wire signature format: want %q, got %q", ssh.KeyAlgoRSASHA256, sig.Format)
	}
	if err := pub.Verify(data, sig); err != nil {
		t.Errorf("wire verify: %v", err)
	}

	// query extension lists names.
	qResp, err := client.Extension(ExtensionQuery, nil)
	if err != nil {
		t.Fatalf("wire query extension: %v", err)
	}
	var wireNames []string
	rest := qResp
	for len(rest) > 0 {
		var s struct {
			Name string
			Rest []byte `ssh:"rest"`
		}
		if err := ssh.Unmarshal(rest, &s); err != nil {
			t.Fatalf("wire query parse: %v", err)
		}
		wireNames = append(wireNames, s.Name)
		rest = s.Rest
	}
	if len(wireNames) == 0 || wireNames[0] != ExtensionQuery {
		t.Errorf("wire query: want %q first, got %v", ExtensionQuery, wireNames)
	}

	// Remove of a missing key fails.
	_, missingPriv, _ := ed25519.GenerateKey(rand.Reader)
	missingSigner, _ := ssh.NewSignerFromKey(missingPriv)
	if err := client.Remove(missingSigner.PublicKey()); err == nil {
		t.Error("wire Remove of missing key: want failure, got nil")
	}

	// Add with confirm constraint fails.
	if err := client.Add(sshagent.AddedKey{PrivateKey: priv, ConfirmBeforeUse: true}); err == nil {
		t.Error("wire Add with confirm constraint: want failure, got nil")
	}

	// Add with a lifetime constraint is accepted and the key is listed.
	_, lifetimePriv, _ := ed25519.GenerateKey(rand.Reader)
	if err := client.Add(sshagent.AddedKey{PrivateKey: lifetimePriv, Comment: "expiring", LifetimeSecs: 2}); err != nil {
		t.Fatalf("wire Add with lifetime: %v", err)
	}
	keysWithLifetime, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	foundLifetime := false
	for _, k := range keysWithLifetime {
		if k.Comment == "expiring" {
			foundLifetime = true
		}
	}
	if !foundLifetime {
		t.Error("wire Add with lifetime: expiring key not listed")
	}

	// Lock / unlock state over the wire.
	if err := client.Lock(nil); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if err := client.Lock(nil); err == nil {
		t.Error("second Lock: want failure, got nil")
	}
	keys, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Errorf("List while locked: want 0 keys, got %d", len(keys))
	}
	if err := client.Unlock([]byte("wrong-passphrase")); err == nil {
		t.Error("Unlock wrong passphrase: want failure, got nil")
	}
	if err := client.Unlock(passphrase); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if err := client.Unlock(passphrase); err == nil {
		t.Error("Unlock while unlocked: want failure, got nil")
	}
}
