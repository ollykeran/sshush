package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/keys"
	ssh "golang.org/x/crypto/ssh"
)

func TestRunValidate_ed25519(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "validate-ed25519")

	if err := runValidate(privPath); err != nil {
		t.Fatalf("runValidate: %v", err)
	}
}

func TestRunValidate_rsa(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := filepath.Join(dir, "id_rsa")
	if err := runCreate("rsa", 2048, "validate-rsa", privPath, false); err != nil {
		t.Fatal(err)
	}

	if err := runValidate(privPath); err != nil {
		t.Fatalf("runValidate: %v", err)
	}
}

func TestRunValidate_ecdsa(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := filepath.Join(dir, "id_ecdsa")
	if err := runCreate("ecdsa", 256, "validate-ecdsa", privPath, false); err != nil {
		t.Fatal(err)
	}

	if err := runValidate(privPath); err != nil {
		t.Fatalf("runValidate: %v", err)
	}
}

func TestRunValidate_missingFile(t *testing.T) {
	t.Parallel()
	err := runValidate(filepath.Join(t.TempDir(), "nonexistent"))
	if err == nil {
		t.Fatal("expected error for missing key file")
	}
}

func TestRunValidate_notOpenSSHKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	badPath := filepath.Join(dir, "bad_key")
	os.WriteFile(badPath, []byte("not a real key"), 0o600)

	err := runValidate(badPath)
	if err == nil {
		t.Fatal("expected error for non-OpenSSH key")
	}
}

func TestRunValidate_pubFileMatches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "pub-match")

	// writeTestKey already creates .pub, so it should match.
	if err := runValidate(privPath); err != nil {
		t.Fatalf("runValidate with matching .pub: %v", err)
	}
}

func TestRunValidate_pubFileMismatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "pub-mismatch-test")

	// Overwrite .pub with a different key's authorized_keys line.
	pubPath := privPath + ".pub"
	otherPrivPath := writeTestKey(t, dir, "other", "other-key")
	otherPubData, err := os.ReadFile(otherPrivPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pubPath, otherPubData, 0o644); err != nil {
		t.Fatal(err)
	}

	// runValidate should succeed (it doesn't error on mismatch, just warns).
	if err := runValidate(privPath); err != nil {
		t.Fatalf("runValidate should not error on .pub mismatch: %v", err)
	}
}

func TestRunValidate_noPubFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "no-pub")

	// Remove the .pub file.
	os.Remove(privPath + ".pub")

	if err := runValidate(privPath); err != nil {
		t.Fatalf("runValidate without .pub: %v", err)
	}
}

func TestRunValidate_fingerprintMatchesSigner(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "fp-check")

	// Parse key and compute expected fingerprint.
	data, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatal(err)
	}
	rawKey, err := ssh.ParseRawPrivateKey(data)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(rawKey)
	if err != nil {
		t.Fatal(err)
	}
	expectedFP := ssh.FingerprintSHA256(signer.PublicKey())

	// Load via LoadKeyMaterial and compare.
	parsed, _, loadedSigner, err := keys.LoadKeyMaterial(privPath)
	if err != nil {
		t.Fatal(err)
	}
	gotFP := ssh.FingerprintSHA256(loadedSigner.PublicKey())
	if gotFP != expectedFP {
		t.Errorf("fingerprint mismatch: got %s, want %s", gotFP, expectedFP)
	}
	if parsed.Comment != "fp-check" {
		t.Errorf("comment: got %q, want %q", parsed.Comment, "fp-check")
	}
}

func TestKeySizeInfo_ed25519(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "")

	data, _ := os.ReadFile(privPath)
	rawKey, _ := ssh.ParseRawPrivateKey(data)
	signer, _ := ssh.NewSignerFromKey(rawKey)

	bits, curve := keySizeInfo(signer.PublicKey())
	if bits != 256 {
		t.Errorf("ed25519 bits: got %d, want 256", bits)
	}
	if curve != "" {
		t.Errorf("ed25519 curve: got %q, want empty", curve)
	}
}

func TestKeySizeInfo_ecdsa256(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := filepath.Join(dir, "id_ecdsa")
	if err := runCreate("ecdsa", 256, "", privPath, false); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(privPath)
	rawKey, _ := ssh.ParseRawPrivateKey(data)
	signer, _ := ssh.NewSignerFromKey(rawKey)

	bits, curve := keySizeInfo(signer.PublicKey())
	if bits != 256 {
		t.Errorf("ecdsa-256 bits: got %d, want 256", bits)
	}
	if curve != "nistp256" {
		t.Errorf("ecdsa-256 curve: got %q, want nistp256", curve)
	}
}

func TestKeySizeInfo_rsa(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := filepath.Join(dir, "id_rsa")
	if err := runCreate("rsa", 2048, "", privPath, false); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(privPath)
	rawKey, _ := ssh.ParseRawPrivateKey(data)
	signer, _ := ssh.NewSignerFromKey(rawKey)

	bits, curve := keySizeInfo(signer.PublicKey())
	if bits != 2048 {
		t.Errorf("rsa bits: got %d, want 2048", bits)
	}
	if curve != "" {
		t.Errorf("rsa curve: got %q, want empty", curve)
	}
}

func TestValidateCommand_zeroArgs(t *testing.T) {
	t.Parallel()
	cmd := newValidateCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for zero args")
	}
	if !strings.Contains(err.Error(), "key file path is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunValidate_pubKey_matchesPrivate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "pubkey-match")
	pubPath := privPath + ".pub"

	if err := runValidate(pubPath); err != nil {
		t.Fatalf("runValidate(pub): %v", err)
	}
}

func TestRunValidate_pubKey_noPrivate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "orphan-pub")
	pubPath := privPath + ".pub"
	os.Remove(privPath)

	if err := runValidate(pubPath); err != nil {
		t.Fatalf("runValidate(pub without priv): %v", err)
	}
}

func TestRunValidate_pubKey_mismatchedPrivate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath1 := writeTestKey(t, dir, "id_ed25519", "key-one")
	privPath2 := writeTestKey(t, dir, "id_ed25519", "key-two")

	// Overwrite key-one's .pub with key-two's public key.
	pubPath1 := privPath1 + ".pub"
	otherPubData, err := os.ReadFile(privPath2 + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pubPath1, otherPubData, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runValidate(pubPath1); err != nil {
		t.Fatalf("runValidate(pub mismatch): %v", err)
	}
}

func TestRunValidate_pubKey_invalid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	badPubPath := filepath.Join(dir, "bad.pub")
	os.WriteFile(badPubPath, []byte("not a public key"), 0o644)

	err := runValidate(badPubPath)
	if err == nil {
		t.Fatal("expected error for invalid public key")
	}
	if !strings.Contains(err.Error(), "invalid public key") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunValidate_pubKey_missing(t *testing.T) {
	t.Parallel()
	err := runValidate(filepath.Join(t.TempDir(), "nonexistent.pub"))
	if err == nil {
		t.Fatal("expected error for missing pub file")
	}
}

func TestRunValidate_pubKey_rsa(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := filepath.Join(dir, "id_rsa")
	if err := runCreate("rsa", 2048, "pubkey-rsa", privPath, false); err != nil {
		t.Fatal(err)
	}

	if err := runValidate(privPath + ".pub"); err != nil {
		t.Fatalf("runValidate(rsa pub): %v", err)
	}
}

func TestRunValidate_pubKey_ecdsa(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := filepath.Join(dir, "id_ecdsa")
	if err := runCreate("ecdsa", 256, "pubkey-ecdsa", privPath, false); err != nil {
		t.Fatal(err)
	}

	if err := runValidate(privPath + ".pub"); err != nil {
		t.Fatalf("runValidate(ecdsa pub): %v", err)
	}
}

func TestRunValidate_pubKey_comment(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	privPath := writeTestKey(t, dir, "id_ed25519", "comment-test")
	pubPath := privPath + ".pub"

	// runValidate(pub) should show comment from the private key.
	if err := runValidate(pubPath); err != nil {
		t.Fatalf("runValidate(pub with comment): %v", err)
	}
}
