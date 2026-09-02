package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/keys"
	"golang.org/x/crypto/argon2"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// callOp performs op directly against va, without a socket, and returns the
// reason it failed. Extension never errors for an op: the reason is in the body.
func callOp(t *testing.T, va *VaultAgent, op byte, payload []byte) ([]byte, error) {
	t.Helper()
	resp, err := va.Extension(agent.ExtensionOp, agent.EncodeOpRequest(op, payload))
	if err != nil {
		t.Fatalf("op extension returned an error rather than a status: %v", err)
	}
	return agent.DecodeOpResponse(resp)
}
func unixSocketTempDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "sshush-a-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

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

// setupBenchAgent returns an unlocked vault ExtendedAgent for benchmarks.
func setupBenchAgent(b *testing.B) (sshagent.ExtendedAgent, func()) {
	b.Helper()
	dir := b.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := Open(vaultPath)
	if err != nil {
		b.Fatal(err)
	}
	passphrase := []byte("bench-passphrase")
	if err := Init(store, passphrase); err != nil {
		b.Fatal(err)
	}
	va := NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		b.Fatal(err)
	}
	return va, func() {}
}

func BenchmarkUnlockChain_Raw(b *testing.B) {
	dir := b.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := Open(vaultPath)
	if err != nil {
		b.Fatal(err)
	}
	passphrase := []byte("bench-unlock-pass")
	if err := Init(store, passphrase); err != nil {
		b.Fatal(err)
	}
	meta := store.GetMetadata()
	salt := meta.Salt
	canary := meta.Canary
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		masterKey := argon2.IDKey(passphrase, salt, 3, 64*1024, 1, 32)
		block, _ := aes.NewCipher(masterKey)
		aead, _ := cipher.NewGCM(block)
		iv, ct := canary[:gcmIVSize], canary[gcmIVSize:]
		canaryPlain, _ := aead.Open(nil, iv, ct, nil)
		_ = subtle.ConstantTimeCompare(canaryPlain, []byte(canaryPlaintext))
		wipe(masterKey)
		wipe(canaryPlain)
	}
}

func BenchmarkVaultUnlock_Sshush(b *testing.B) {
	dir := b.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := Open(vaultPath)
	if err != nil {
		b.Fatal(err)
	}
	passphrase := []byte("bench-unlock-pass")
	if err := Init(store, passphrase); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		va := NewVaultAgent(store)
		if err := va.Unlock(passphrase); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSignChain_Raw(b *testing.B) {
	dir := b.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := Open(vaultPath)
	if err != nil {
		b.Fatal(err)
	}
	passphrase := []byte("bench-sign-pass")
	if err := Init(store, passphrase); err != nil {
		b.Fatal(err)
	}
	va := NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		b.Fatal(err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}
	sshPub, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		b.Fatal(err)
	}
	if err := va.Add(sshagent.AddedKey{PrivateKey: priv, Comment: "bench-sign"}); err != nil {
		b.Fatal(err)
	}
	fp := fingerprint(sshPub.PublicKey())
	encrypted, _, _ := store.GetIdentity(fp)
	masterKey := va.masterKey
	data := []byte("benchmark data to sign for SSH agent performance test")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		plain, _ := decryptBlob(masterKey, encrypted)
		priv, _ := unmarshalPrivateKey(plain, "ssh-ed25519")
		signer, _ := ssh.NewSignerFromKey(priv)
		_, _ = signer.Sign(nil, data)
		wipe(plain)
	}
}

func BenchmarkVaultSign_Sshush(b *testing.B) {
	ext, _ := setupBenchAgent(b)
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}
	sshPub, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		b.Fatal(err)
	}
	if err := ext.Add(sshagent.AddedKey{PrivateKey: priv, Comment: "bench-sign"}); err != nil {
		b.Fatal(err)
	}
	data := []byte("benchmark data to sign for SSH agent performance test")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = ext.Sign(sshPub.PublicKey(), data)
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

			sockDir := unixSocketTempDir(t)
			socketPath := filepath.Join(sockDir, "agent.sock")
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
	if err := va.addKeyWithAutoload(sshagent.AddedKey{PrivateKey: privB, Comment: "autoload"}, true, ""); err != nil {
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
	resp, err := callOp(t, va, agent.OpAddKey, payload)
	if err != nil {
		t.Fatal(err)
	}
	// The op carries success in its status byte, so a successful add has no body.
	// The legacy extension used to answer the literal "ok".
	if len(resp) != 0 {
		t.Errorf("add-key response body: want empty, got %q", string(resp))
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

func TestExtension_VaultSessionLoad(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("session-load-test")
	if err := Init(store, passphrase); err != nil {
		t.Fatal(err)
	}

	va := NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	if err := va.addKeyWithAutoload(sshagent.AddedKey{PrivateKey: priv, Comment: "noload"}, false, ""); err != nil {
		t.Fatal(err)
	}

	va2 := NewVaultAgent(store)
	if err := va2.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}
	keys, err := va2.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("before session-load: want 0 listed keys, got %d", len(keys))
	}

	signer, _ := ssh.NewSignerFromKey(priv)
	fp := fingerprint(signer.PublicKey())
	_, err = callOp(t, va2, agent.OpSessionLoad, []byte(fp))
	if err != nil {
		t.Fatal(err)
	}
	keys2, err := va2.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys2) != 1 {
		t.Fatalf("after session-load: want 1 key, got %d", len(keys2))
	}

	// No-op for autoload identity
	_, privB, _ := ed25519.GenerateKey(rand.Reader)
	if err := va2.addKeyWithAutoload(sshagent.AddedKey{PrivateKey: privB, Comment: "autoloaded"}, true, ""); err != nil {
		t.Fatal(err)
	}
	signerB, _ := ssh.NewSignerFromKey(privB)
	fpB := fingerprint(signerB.PublicKey())
	if _, err := callOp(t, va2, agent.OpSessionLoad, []byte(fpB)); err != nil {
		t.Fatalf("session-load autoload key should no-op: %v", err)
	}
}

func TestExtension_VaultSetAutoload(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("set-autoload-test")
	if err := Init(store, passphrase); err != nil {
		t.Fatal(err)
	}

	va := NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	if err := va.addKeyWithAutoload(sshagent.AddedKey{PrivateKey: priv, Comment: "toggle"}, false, ""); err != nil {
		t.Fatal(err)
	}
	signer, _ := ssh.NewSignerFromKey(priv)
	fp := fingerprint(signer.PublicKey())

	payload := BuildSetAutoloadPayload(fp, true)
	if _, err := callOp(t, va, agent.OpSetAutoload, payload); err != nil {
		t.Fatal(err)
	}

	va2 := NewVaultAgent(store)
	if err := va2.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}
	keys, _ := va2.List()
	if len(keys) != 1 {
		t.Fatalf("after set autoload on: want 1 key at restart, got %d", len(keys))
	}

	payloadOff := BuildSetAutoloadPayload(fp, false)
	if _, err := callOp(t, va2, agent.OpSetAutoload, payloadOff); err != nil {
		t.Fatal(err)
	}
	va3 := NewVaultAgent(store)
	if err := va3.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}
	keys3, _ := va3.List()
	if len(keys3) != 0 {
		t.Fatalf("after set autoload off: want 0 keys at restart, got %d", len(keys3))
	}
}

func TestExtension_VaultSetComment(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("set-comment-test")
	if err := Init(store, passphrase); err != nil {
		t.Fatal(err)
	}

	va := NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	if err := va.addKeyWithAutoload(sshagent.AddedKey{PrivateKey: priv, Comment: "before"}, true, ""); err != nil {
		t.Fatal(err)
	}
	signer, _ := ssh.NewSignerFromKey(priv)
	fp := fingerprint(signer.PublicKey())

	payload := BuildSetCommentPayload(fp, "after")
	if _, err := callOp(t, va, agent.OpSetComment, payload); err != nil {
		t.Fatal(err)
	}

	ids := store.AllIdentities()
	if len(ids) != 1 {
		t.Fatalf("want 1 identity, got %d", len(ids))
	}
	if ids[0].Comment != "after" {
		t.Fatalf("Identity.Comment = %q, want %q", ids[0].Comment, "after")
	}
}

func TestVaultAgent_SetComment_lockedFails(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := Init(store, []byte("locked-test")); err != nil {
		t.Fatal(err)
	}

	va := NewVaultAgent(store)
	if err := va.setIdentityComment("SHA256:anything", "new"); err != errLocked {
		t.Fatalf("setIdentityComment on locked vault: got %v, want errLocked", err)
	}
}

func TestVaultAgent_SetComment_unknownFingerprintFails(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("unknown-fp-test")
	if err := Init(store, passphrase); err != nil {
		t.Fatal(err)
	}
	va := NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}

	if err := va.setIdentityComment("SHA256:doesnotexist", "new"); err != errKeyNotFound {
		t.Fatalf("setIdentityComment for unknown fingerprint: got %v, want errKeyNotFound", err)
	}
}

func TestExtension_VaultSetComment_malformedPayload(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("malformed-test")
	if err := Init(store, passphrase); err != nil {
		t.Fatal(err)
	}
	va := NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}

	if _, err := callOp(t, va, agent.OpSetComment, []byte("short")); err == nil {
		t.Fatal("expected error for too-short payload")
	}

	// Well-formed header but truncated comment bytes.
	payload := BuildSetCommentPayload("fp", "comment")
	truncated := payload[:len(payload)-2]
	if _, err := callOp(t, va, agent.OpSetComment, truncated); err == nil {
		t.Fatal("expected error for truncated comment payload")
	}
}

func TestAddKeyWithAutoload_storesFilepath(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("filepath-test")
	if err := Init(store, passphrase); err != nil {
		t.Fatal(err)
	}

	va := NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	keyFilepath := "/home/user/.ssh/id_ed25519"
	if err := va.addKeyWithAutoload(sshagent.AddedKey{PrivateKey: priv, Comment: "with-path"}, true, keyFilepath); err != nil {
		t.Fatal(err)
	}

	ids := store.AllIdentities()
	if len(ids) != 1 {
		t.Fatalf("want 1 identity, got %d", len(ids))
	}
	if ids[0].Filepath != keyFilepath {
		t.Fatalf("Identity.Filepath = %q, want %q", ids[0].Filepath, keyFilepath)
	}
}

func TestAddKeyWithAutoload_emptyFilepath(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.json")
	store, err := Open(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("no-path-test")
	if err := Init(store, passphrase); err != nil {
		t.Fatal(err)
	}

	va := NewVaultAgent(store)
	if err := va.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	if err := va.addKeyWithAutoload(sshagent.AddedKey{PrivateKey: priv, Comment: "no-path"}, false, ""); err != nil {
		t.Fatal(err)
	}

	ids := store.AllIdentities()
	if len(ids) != 1 {
		t.Fatalf("want 1 identity, got %d", len(ids))
	}
	if ids[0].Filepath != "" {
		t.Fatalf("Identity.Filepath = %q, want empty", ids[0].Filepath)
	}
}
