package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ollykeran/sshush/internal/vault"
	"golang.org/x/crypto/ssh"
)

// dialWithPassword connects to addr offering only password authentication.
func dialWithPassword(addr, password string) (*ssh.Client, error) {
	return ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            "test",
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	})
}

func TestServer_SignsInWithTheVaultPassphrase(t *testing.T) {
	vaultPath := initTestVault(t)
	addr, _ := startShellServer(t, func(s *Server) {
		s.Passwords = &VaultPassphraseAuth{VaultPath: vaultPath}
	})

	conn, err := dialWithPassword(addr, testVaultPassphrase)
	if err != nil {
		t.Fatalf("dial with the vault passphrase: %v", err)
	}
	defer conn.Close()

	stdout, stderr, err := runCommand(t, conn, nil, "echo hi")
	if err != nil {
		t.Fatalf("echo hi: %v (stderr %q)", err, stderr)
	}
	if stdout != "hi\n" {
		t.Errorf("stdout = %q, want %q", stdout, "hi\n")
	}
}

func TestServer_RefusesAWrongPassword(t *testing.T) {
	vaultPath := initTestVault(t)
	addr, _ := startShellServer(t, func(s *Server) {
		s.Passwords = &VaultPassphraseAuth{VaultPath: vaultPath}
	})

	conn, err := dialWithPassword(addr, "not the passphrase")
	if err == nil {
		conn.Close()
		t.Fatal("a wrong password signed in")
	}
}

// TestServer_OffersNoPasswordAuthUnlessEnabled checks password authentication is
// not merely failing but absent: the client is never invited to try one.
func TestServer_OffersNoPasswordAuthUnlessEnabled(t *testing.T) {
	addr, _ := startShellServer(t)

	conn, err := dialWithPassword(addr, testVaultPassphrase)
	if err == nil {
		conn.Close()
		t.Fatal("signed in with a password on a server without password authentication")
	}
	if strings.Contains(err.Error(), "password") {
		t.Errorf("dial error = %q; want password never attempted, since it was never offered", err)
	}
}

func TestServer_KeysStillSignInWithPasswordsOn(t *testing.T) {
	vaultPath := initTestVault(t)
	addr, signer := startShellServer(t, func(s *Server) {
		s.Passwords = &VaultPassphraseAuth{VaultPath: vaultPath}
	})
	dialShellServer(t, addr, signer).Close()
}

const testVaultPassphrase = "e2epassthatalwaysmeetspasswordrequirementsA1!"

// initTestVault creates a vault with testVaultPassphrase and returns its path.
func initTestVault(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vault.json")
	store, err := vault.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(store, []byte(testVaultPassphrase)); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVaultPassphraseAuth_AcceptsTheVaultsPassphrase(t *testing.T) {
	auth := &VaultPassphraseAuth{VaultPath: initTestVault(t)}
	if !auth.Verify([]byte(testVaultPassphrase)) {
		t.Error("the vault's passphrase was refused")
	}
}

func TestVaultPassphraseAuth_RejectsAWrongPassphrase(t *testing.T) {
	auth := &VaultPassphraseAuth{VaultPath: initTestVault(t)}
	if auth.Verify([]byte("not the passphrase")) {
		t.Error("a wrong passphrase was accepted")
	}
}

func TestVaultPassphraseAuth_AcceptsNothingWithoutAVault(t *testing.T) {
	if (&VaultPassphraseAuth{}).Verify([]byte(testVaultPassphrase)) {
		t.Error("no vault path, yet a passphrase was accepted")
	}

	dir := filepath.Join(t.TempDir(), "absent")
	auth := &VaultPassphraseAuth{VaultPath: filepath.Join(dir, "vault.json")}
	if auth.Verify([]byte(testVaultPassphrase)) {
		t.Error("a missing vault accepted a passphrase")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("checking a password against a missing vault created %s", dir)
	}
}
