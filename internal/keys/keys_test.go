package keys

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ssh "golang.org/x/crypto/ssh"
)

func TestGenerate(t *testing.T) {
	tests := []struct {
		name       string
		keyType    string
		bits       int
		wantPrefix string
		comment    string
	}{
		{name: "ed25519", keyType: "ed25519", wantPrefix: "ssh-ed25519", comment: "test@host"},
		{name: "rsa2048", keyType: "rsa", bits: 2048, wantPrefix: "ssh-rsa", comment: "rsa-test"},
		{name: "ecdsa256", keyType: "ecdsa", bits: 256, wantPrefix: "ecdsa-sha2-nistp256", comment: "ec-test"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			priv, pub, err := Generate(tc.keyType, tc.bits, tc.comment)
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}
			if len(priv) == 0 || len(pub) == 0 {
				t.Fatalf("Generate() returned empty key material")
			}
			raw, err := ssh.ParseRawPrivateKey(priv)
			if err != nil {
				t.Fatalf("parse private key: %v", err)
			}
			signer, err := ssh.NewSignerFromKey(raw)
			if err != nil {
				t.Fatalf("create signer: %v", err)
			}
			if got := signer.PublicKey().Type(); got != tc.wantPrefix {
				t.Fatalf("public key type = %q, want %q", got, tc.wantPrefix)
			}
			if !strings.Contains(string(pub), tc.comment) {
				t.Fatalf("public line missing comment %q: %s", tc.comment, string(pub))
			}
		})
	}
}

func TestGenerateUnsupportedType(t *testing.T) {
	if _, _, err := Generate("dsa", 0, "x"); err == nil {
		t.Fatal("expected error for unsupported key type")
	}
}

func TestGenerate_rsaRejectsWeakOrNonstandardSize(t *testing.T) {
	t.Parallel()
	for _, bits := range []int{512, 1024, 1536, 8192} {
		t.Run(fmt.Sprintf("bits_%d", bits), func(t *testing.T) {
			_, _, err := Generate("rsa", bits, "x")
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestSavePair(t *testing.T) {
	dir := t.TempDir()
	priv, pub, err := Generate("ed25519", 0, "save-test")
	if err != nil {
		t.Fatalf("Generate(): %v", err)
	}
	if err := SavePair(dir, "id_test", priv, pub); err != nil {
		t.Fatalf("SavePair(): %v", err)
	}

	privPath := filepath.Join(dir, "id_test")
	pubPath := privPath + ".pub"

	privInfo, err := os.Stat(privPath)
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if got := privInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("private permissions = %o, want 600", got)
	}

	pubInfo, err := os.Stat(pubPath)
	if err != nil {
		t.Fatalf("stat public key: %v", err)
	}
	if got := pubInfo.Mode().Perm(); got != 0o644 {
		t.Fatalf("public permissions = %o, want 644", got)
	}
}

func TestLoadKeyMaterialAndSaveWithComment(t *testing.T) {
	dir := t.TempDir()
	priv, pub, err := Generate("ed25519", 0, "old-comment")
	if err != nil {
		t.Fatalf("Generate(): %v", err)
	}
	if err := SavePair(dir, "id_ed25519", priv, pub); err != nil {
		t.Fatalf("SavePair(): %v", err)
	}

	keyPath := filepath.Join(dir, "id_ed25519")
	parsed, raw, signer, err := LoadKeyMaterial(keyPath)
	if err != nil {
		t.Fatalf("LoadKeyMaterial(): %v", err)
	}
	if parsed.Comment != "old-comment" {
		t.Fatalf("parsed comment = %q, want old-comment", parsed.Comment)
	}
	if raw == nil || signer == nil {
		t.Fatal("expected raw key and signer")
	}

	if err := SaveWithComment(raw, "new-comment", keyPath); err != nil {
		t.Fatalf("SaveWithComment(): %v", err)
	}

	pubData, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("read updated pub key: %v", err)
	}
	if !strings.Contains(string(pubData), "new-comment") {
		t.Fatalf(".pub comment not updated: %s", string(pubData))
	}
}

func TestFormatPublicKey(t *testing.T) {
	priv, _, err := Generate("ed25519", 0, "fmt")
	if err != nil {
		t.Fatalf("Generate(): %v", err)
	}
	raw, err := ssh.ParseRawPrivateKey(priv)
	if err != nil {
		t.Fatalf("ParseRawPrivateKey(): %v", err)
	}
	signer, err := ssh.NewSignerFromKey(raw)
	if err != nil {
		t.Fatalf("NewSignerFromKey(): %v", err)
	}

	got := FormatPublicKey(signer, "formatted-comment")
	if !strings.HasSuffix(got, "formatted-comment\n") {
		t.Fatalf("FormatPublicKey() = %q, expected comment suffix", got)
	}
}
