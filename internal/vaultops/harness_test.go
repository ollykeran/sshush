package vaultops

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/keys"
	"github.com/ollykeran/sshush/internal/vault"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// unixSocketTempDirVaultOps returns a temp directory with a short path so unix
// socket paths fit sockaddr_un; macOS limits sun_path to 103 bytes and
// t.TempDir() under /var/folders/... often exceeds that.
func unixSocketTempDirVaultOps(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "sshush-vaultops-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// serve runs keyring on a fresh socket under dir until the test ends.
func serve(t *testing.T, dir, name string, keyring sshagent.ExtendedAgent) string {
	t.Helper()
	socketPath := filepath.Join(dir, name)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
		_ = os.Remove(socketPath)
	})
	ready := make(chan struct{})
	go func() {
		_ = agent.ListenAndServe(ctx, socketPath, keyring, agent.WithReady(func() { close(ready) }))
	}()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatalf("agent on %s did not become ready", socketPath)
	}
	return socketPath
}

// fixture is an initialized vault served by a live agent on a temp socket.
type fixture struct {
	dir        string
	socketPath string
	vaultPath  string
	store      *vault.VaultStore
	va         *vault.VaultAgent
	pass       []byte
	mnemonic   string // empty unless the vault was built with recovery
	ask        *countingAsk
}

// startVaultAgent builds an initialized, unlocked vault and serves it.
func startVaultAgent(t *testing.T, withRecovery bool) *fixture {
	t.Helper()
	dir := unixSocketTempDirVaultOps(t)
	f := &fixture{
		dir:       dir,
		vaultPath: filepath.Join(dir, "vault.json"),
		pass:      []byte("vaultops-test"),
		ask:       &countingAsk{pass: []byte("vaultops-test")},
	}
	store, err := vault.Open(f.vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(store, f.pass); err != nil {
		t.Fatal(err)
	}
	if withRecovery {
		f.mnemonic, err = vault.GenerateRecoveryMnemonic()
		if err != nil {
			t.Fatal(err)
		}
		if err := vault.EnableRecoveryWithPassphrase(store, f.pass, f.mnemonic); err != nil {
			t.Fatal(err)
		}
	}
	f.store = store
	f.va = vault.NewVaultAgent(store)
	if err := f.va.Unlock(f.pass); err != nil {
		t.Fatal(err)
	}
	f.socketPath = serve(t, dir, "agent.sock", f.va)
	return f
}

// env is the Env a front end that can prompt would build for this vault.
func (f *fixture) env() Env {
	return Env{
		VaultPath:      f.vaultPath,
		SocketPath:     f.socketPath,
		AgentVaultPath: f.vaultPath,
		AskPassphrase:  f.ask.fn,
	}
}

// silentEnv is the Env the TUI builds: no prompting, no agent vault path.
func (f *fixture) silentEnv() Env {
	return Env{VaultPath: f.vaultPath, SocketPath: f.socketPath}
}

// lock wipes the agent's master key, as 'sshush lock' does.
func (f *fixture) lock(t *testing.T) {
	t.Helper()
	if err := f.va.Lock(nil); err != nil {
		t.Fatalf("lock vault agent: %v", err)
	}
}

// addKey stores a freshly generated key in the vault through the agent.
func (f *fixture) addKey(t *testing.T, filename, comment string, autoload bool) (path, fingerprint string) {
	t.Helper()
	path, fingerprint = writeTestKey(t, f.dir, filename, comment)
	if _, err := Add(f.env(), []string{path}, autoload); err != nil {
		t.Fatalf("seed key %s: %v", comment, err)
	}
	return path, fingerprint
}

// startKeyringAgent serves a keys-mode sshush agent: it speaks sshush-op but
// answers the vault operations unknown.
func startKeyringAgent(t *testing.T, dir string) string {
	t.Helper()
	inner, ok := sshagent.NewKeyring().(sshagent.ExtendedAgent)
	if !ok {
		t.Fatal("keyring is not an ExtendedAgent")
	}
	return serve(t, dir, "keyring.sock", agent.NewKDFLockedKeyring(inner))
}

// startForeignAgent serves a plain ssh-agent, which does not speak sshush-op at
// all — the case a stale sshushd also presents.
func startForeignAgent(t *testing.T, dir string) string {
	t.Helper()
	inner, ok := sshagent.NewKeyring().(sshagent.ExtendedAgent)
	if !ok {
		t.Fatal("keyring is not an ExtendedAgent")
	}
	return serve(t, dir, "foreign.sock", inner)
}

// writeTestKey writes a freshly generated ed25519 private key and returns its
// path and SHA256 fingerprint.
func writeTestKey(t *testing.T, dir, filename, comment string) (path, fingerprint string) {
	t.Helper()
	privPEM, _, err := keys.Generate("ed25519", 0, comment)
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(dir, filename)
	if err := os.WriteFile(path, privPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.ParsePrivateKey(privPEM)
	if err != nil {
		t.Fatal(err)
	}
	return path, ssh.FingerprintSHA256(signer.PublicKey())
}

// countingAsk is a PassphraseFunc that records how often it was called, so a
// test can assert a verb prompted exactly once, or not at all.
type countingAsk struct {
	calls   int
	prompts []string
	pass    []byte
	err     error
}

func (c *countingAsk) fn(prompt string) ([]byte, error) {
	c.calls++
	c.prompts = append(c.prompts, prompt)
	if c.err != nil {
		return nil, c.err
	}
	// The caller zeroes what it is given, so hand out a copy.
	out := make([]byte, len(c.pass))
	copy(out, c.pass)
	return out, nil
}
