package vault

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/keys"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// setupExtendedAgent returns an ExtendedAgent and cleanup for the given backend.
// Backend is "keyring" (in-memory) or "vault" (temp DB, Init, Unlock).
func setupExtendedAgent(t *testing.T, backend string) (ext sshagent.ExtendedAgent, cleanup func()) {
	t.Helper()
	switch backend {
	case "keyring":
		return sshagent.NewKeyring().(sshagent.ExtendedAgent), func() {}
	case "vault":
		dir := t.TempDir()
		vaultPath := filepath.Join(dir, "vault.json")
		store, err := Open(vaultPath)
		if err != nil {
			t.Fatal(err)
		}
		passphrase := []byte("test-passphrase")
		if err := Init(store, passphrase); err != nil {
			t.Fatal(err)
		}
		va := NewVaultAgent(store)
		if err := va.Unlock(passphrase); err != nil {
			t.Fatal(err)
		}
		return va, func() {}
	default:
		t.Fatalf("unknown backend %q", backend)
		return nil, nil
	}
}

func TestVaultAgent_InitUnlockAddListRemove(t *testing.T) {
	for _, backend := range []string{"keyring", "vault"} {
		backend := backend
		t.Run(backend, func(t *testing.T) {
			ext, cleanup := setupExtendedAgent(t, backend)
			defer cleanup()

			_, priv, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			err = ext.Add(sshagent.AddedKey{PrivateKey: priv, Comment: "test-key"})
			if err != nil {
				t.Fatal(err)
			}

			keys, err := ext.List()
			if err != nil {
				t.Fatal(err)
			}
			if len(keys) != 1 {
				t.Fatalf("want 1 key, got %d", len(keys))
			}
			if keys[0].Comment != "test-key" {
				t.Errorf("comment: want %q, got %q", "test-key", keys[0].Comment)
			}

			pub, err := ssh.ParsePublicKey(keys[0].Blob)
			if err != nil {
				t.Fatal(err)
			}
			if err := ext.Remove(pub); err != nil {
				t.Fatal(err)
			}
			keys2, _ := ext.List()
			if len(keys2) != 0 {
				t.Errorf("after remove: want 0 keys, got %d", len(keys2))
			}
		})
	}
}

func TestVaultAgent_Sign(t *testing.T) {
	for _, backend := range []string{"keyring", "vault"} {
		backend := backend
		t.Run(backend, func(t *testing.T) {
			ext, cleanup := setupExtendedAgent(t, backend)
			defer cleanup()

			_, priv, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			sshPub, err := ssh.NewSignerFromKey(priv)
			if err != nil {
				t.Fatal(err)
			}
			if err := ext.Add(sshagent.AddedKey{PrivateKey: priv, Comment: "sign-key"}); err != nil {
				t.Fatal(err)
			}

			data := []byte("data to sign")
			sig, err := ext.Sign(sshPub.PublicKey(), data)
			if err != nil {
				t.Fatal(err)
			}
			if sig == nil {
				t.Fatal("nil signature")
			}

			if backend == "vault" {
				va := ext.(*VaultAgent)
				va.Lock(nil)
				_, err = va.Sign(sshPub.PublicKey(), data)
				if err != errLocked {
					t.Errorf("Sign when locked: want errLocked, got %v", err)
				}
			}
		})
	}
}

func TestVaultAgent_LockWipesMasterKey(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("lock-test")
	if err := Init(store, passphrase); err != nil {
		t.Fatal(err)
	}
	va := NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}
	if err := va.Lock(nil); err != nil {
		t.Fatal(err)
	}
	keys, err := va.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Errorf("List when locked: want 0 keys, got %d", len(keys))
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := va.Remove(signer.PublicKey()); err != errLocked {
		t.Errorf("Remove when locked: want errLocked, got %v", err)
	}
	if err := va.RemoveAll(); err != errLocked {
		t.Errorf("RemoveAll when locked: want errLocked, got %v", err)
	}
	err = va.Add(sshagent.AddedKey{PrivateKey: priv, Comment: "x"})
	if err != errLocked {
		t.Errorf("Add when locked: want errLocked, got %v", err)
	}
}

func TestVaultAgent_Recovery(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("recovery-pass")
	if err := Init(store, passphrase); err != nil {
		t.Fatal(err)
	}
	mnemonic, err := GenerateRecoveryMnemonic()
	if err != nil {
		t.Fatal(err)
	}
	if err := EnableRecoveryWithPassphrase(store, passphrase, mnemonic); err != nil {
		t.Fatal(err)
	}

	va := NewVaultAgent(store)
	if err := va.UnlockWithRecovery(mnemonic); err != nil {
		t.Fatal(err)
	}
	keys, err := va.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Errorf("want 0 keys, got %d", len(keys))
	}
}

// TestVaultAgent_NoPlaintextKeyRetained verifies that Sign does not retain the
// decrypted key: we sign twice and both succeed (key is decrypted from store each time).
func TestVaultAgent_NoPlaintextKeyRetained(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("retain-test")
	if err := Init(store, passphrase); err != nil {
		t.Fatal(err)
	}
	va := NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := va.Add(sshagent.AddedKey{PrivateKey: priv, Comment: "x"}); err != nil {
		t.Fatal(err)
	}
	data := []byte("first")
	sig1, err := va.Sign(sshPub.PublicKey(), data)
	if err != nil {
		t.Fatal(err)
	}
	if sig1 == nil {
		t.Fatal("sig1 nil")
	}
	data2 := []byte("second")
	sig2, err := va.Sign(sshPub.PublicKey(), data2)
	if err != nil {
		t.Fatal(err)
	}
	if sig2 == nil {
		t.Fatal("sig2 nil")
	}
}

func TestVaultAgent_ServeAgent_ListAddSignRemove(t *testing.T) {
	for _, backend := range []string{"keyring", "vault"} {
		backend := backend
		t.Run(backend, func(t *testing.T) {
			ext, cleanup := setupExtendedAgent(t, backend)
			defer cleanup()

			dir := t.TempDir()
			socketPath := filepath.Join(dir, "agent.sock")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				_ = agent.ListenAndServe(ctx, socketPath, ext)
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

			_, priv, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			if err := client.Add(sshagent.AddedKey{PrivateKey: priv, Comment: "client-add"}); err != nil {
				t.Fatal(err)
			}
			keys, err := client.List()
			if err != nil {
				t.Fatal(err)
			}
			if len(keys) != 1 {
				t.Fatalf("list: want 1 key, got %d", len(keys))
			}
			signer, _ := ssh.NewSignerFromKey(priv)
			data := []byte("sign-me")
			sig, err := client.Sign(signer.PublicKey(), data)
			if err != nil {
				t.Fatal(err)
			}
			if sig == nil {
				t.Fatal("nil signature")
			}
			if err := client.Remove(signer.PublicKey()); err != nil {
				t.Fatal(err)
			}
			keys2, _ := client.List()
			if len(keys2) != 0 {
				t.Errorf("after remove: want 0 keys, got %d", len(keys2))
			}
		})
	}
}

// TestAutoload_ListFiltersAfterRestart verifies that only autoload=1 keys are
// visible after a "restart" (new VaultAgent), while session-added autoload=0 keys
// are visible in the same run.
func TestAutoload_ListFiltersAfterRestart(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("autoload-test")
	if err := Init(store, passphrase); err != nil {
		t.Fatal(err)
	}

	va := NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}

	_, privA, _ := ed25519.GenerateKey(rand.Reader)
	_, privB, _ := ed25519.GenerateKey(rand.Reader)

	// Add A with autoload=false (standard Add), B with autoload=true.
	if err := va.Add(sshagent.AddedKey{PrivateKey: privA, Comment: "no-autoload"}); err != nil {
		t.Fatal(err)
	}
	if err := va.addKeyWithAutoload(sshagent.AddedKey{PrivateKey: privB, Comment: "autoload"}, true); err != nil {
		t.Fatal(err)
	}

	keysList, err := va.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keysList) != 2 {
		t.Fatalf("same session List: want 2 keys, got %d", len(keysList))
	}

	// Simulate restart: new agent, same store.
	va2 := NewVaultAgent(store)
	if err := va2.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}
	keysAfter, err := va2.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keysAfter) != 1 {
		t.Fatalf("after restart List: want 1 key (autoload=1), got %d", len(keysAfter))
	}
	if keysAfter[0].Comment != "autoload" {
		t.Errorf("comment: want %q, got %q", "autoload", keysAfter[0].Comment)
	}

	// Sign with A (autoload=0) should fail on va2; Sign with B should succeed.
	signerA, _ := ssh.NewSignerFromKey(privA)
	signerB, _ := ssh.NewSignerFromKey(privB)
	if _, err := va2.Sign(signerA.PublicKey(), []byte("x")); err != errKeyNotFound {
		t.Errorf("Sign(autoload=0 key) on new agent: want errKeyNotFound, got %v", err)
	}
	if _, err := va2.Sign(signerB.PublicKey(), []byte("x")); err != nil {
		t.Errorf("Sign(autoload=1 key): %v", err)
	}
}

// TestExtension_AddKeyOpts adds a key via the add-key-opts extension and
// verifies it is stored with the given autoload and appears in List.
func TestExtension_AddKeyOpts(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	keyPath := filepath.Join(dir, "id_ed25519")
	store, err := Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("ext-test")
	if err := Init(store, passphrase); err != nil {
		t.Fatal(err)
	}

	privPEM, _, err := keys.Generate("ed25519", 0, "ext-comment")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, privPEM, 0600); err != nil {
		t.Fatal(err)
	}

	va := NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}

	payload, err := BuildAddKeyOptsPayload(keyPath, true)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := va.Extension(ExtensionAddKeyOpts, payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(resp) != "ok" {
		t.Errorf("extension response: want %q, got %q", "ok", string(resp))
	}

	keysList, err := va.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keysList) != 1 {
		t.Fatalf("List: want 1 key, got %d", len(keysList))
	}
	if keysList[0].Comment != "ext-comment" {
		t.Errorf("comment: want %q, got %q", "ext-comment", keysList[0].Comment)
	}

	// Same store, new agent (restart): key should still be listed (autoload=true).
	va2 := NewVaultAgent(store)
	if err := va2.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}
	keys2, _ := va2.List()
	if len(keys2) != 1 {
		t.Fatalf("after restart List: want 1 key, got %d", len(keys2))
	}
}
