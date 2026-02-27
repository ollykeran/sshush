package tui

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGenerateKey_Ed25519(t *testing.T) {
	priv, pub, err := GenerateKey("ed25519", 0, "test@host")
	if err != nil {
		t.Fatalf("GenerateKey ed25519: %v", err)
	}
	verifyKeyPair(t, priv, pub, "ssh-ed25519", "test@host")
}

func TestGenerateKey_RSA2048(t *testing.T) {
	priv, pub, err := GenerateKey("rsa", 2048, "rsa-test")
	if err != nil {
		t.Fatalf("GenerateKey rsa-2048: %v", err)
	}
	verifyKeyPair(t, priv, pub, "ssh-rsa", "rsa-test")
}

func TestGenerateKey_RSA4096(t *testing.T) {
	priv, pub, err := GenerateKey("rsa", 4096, "rsa4096")
	if err != nil {
		t.Fatalf("GenerateKey rsa-4096: %v", err)
	}
	verifyKeyPair(t, priv, pub, "ssh-rsa", "rsa4096")
}

func TestGenerateKey_ECDSA256(t *testing.T) {
	priv, pub, err := GenerateKey("ecdsa", 256, "ec256")
	if err != nil {
		t.Fatalf("GenerateKey ecdsa-256: %v", err)
	}
	verifyKeyPair(t, priv, pub, "ecdsa-sha2-nistp256", "ec256")
}

func TestGenerateKey_ECDSA384(t *testing.T) {
	priv, pub, err := GenerateKey("ecdsa", 384, "ec384")
	if err != nil {
		t.Fatalf("GenerateKey ecdsa-384: %v", err)
	}
	verifyKeyPair(t, priv, pub, "ecdsa-sha2-nistp384", "ec384")
}

func TestGenerateKey_ECDSA521(t *testing.T) {
	priv, pub, err := GenerateKey("ecdsa", 521, "ec521")
	if err != nil {
		t.Fatalf("GenerateKey ecdsa-521: %v", err)
	}
	verifyKeyPair(t, priv, pub, "ecdsa-sha2-nistp521", "ec521")
}

func TestGenerateKey_UnsupportedType(t *testing.T) {
	_, _, err := GenerateKey("dsa", 0, "x")
	if err == nil {
		t.Fatal("expected error for unsupported key type")
	}
}

func verifyKeyPair(t *testing.T, privPEM, pubAuth []byte, expectedType, expectedComment string) {
	t.Helper()

	if len(privPEM) == 0 {
		t.Fatal("private key PEM is empty")
	}
	if len(pubAuth) == 0 {
		t.Fatal("public key auth line is empty")
	}

	rawKey, err := ssh.ParseRawPrivateKey(privPEM)
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(rawKey)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}

	pubKey := signer.PublicKey()
	if pubKey.Type() != expectedType {
		t.Errorf("key type: got %q, want %q", pubKey.Type(), expectedType)
	}

	pubLine := string(pubAuth)
	if !contains(pubLine, expectedComment) {
		t.Errorf("public key line missing comment %q: %s", expectedComment, pubLine)
	}

	// Verify we can parse the authorized_keys line back
	_, _, _, _, err = ssh.ParseAuthorizedKey(pubAuth)
	if err != nil {
		t.Fatalf("parse authorized key: %v", err)
	}
}

func TestSaveKeyPair(t *testing.T) {
	dir := t.TempDir()
	priv, pub, err := GenerateKey("ed25519", 0, "save-test")
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	if err := SaveKeyPair(dir, "testkey", priv, pub); err != nil {
		t.Fatalf("SaveKeyPair: %v", err)
	}

	privPath := filepath.Join(dir, "testkey")
	pubPath := privPath + ".pub"

	privInfo, err := os.Stat(privPath)
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if privInfo.Mode().Perm() != 0600 {
		t.Errorf("private key permissions: got %o, want 0600", privInfo.Mode().Perm())
	}

	pubInfo, err := os.Stat(pubPath)
	if err != nil {
		t.Fatalf("stat public key: %v", err)
	}
	if pubInfo.Mode().Perm() != 0644 {
		t.Errorf("public key permissions: got %o, want 0644", pubInfo.Mode().Perm())
	}

	pubData, err := os.ReadFile(pubPath)
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}
	_, _, _, _, err = ssh.ParseAuthorizedKey(pubData)
	if err != nil {
		t.Fatalf("parse saved public key: %v", err)
	}
}

func TestSaveKeyPair_Subdirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "dir")
	priv, pub, err := GenerateKey("ed25519", 0, "subdir")
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	if err := SaveKeyPair(dir, "mykey", priv, pub); err != nil {
		t.Fatalf("SaveKeyPair: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "mykey")); err != nil {
		t.Fatalf("private key not created: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
