package cli

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	ssh "golang.org/x/crypto/ssh"
)

func TestRunCreate_ed25519(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "id_ed25519")

	if err := runCreate("ed25519", 0, "test-create", out, false); err != nil {
		t.Fatalf("runCreate: %v", err)
	}

	assertKeyPairExists(t, out, 0o600, 0o644)
	assertKeyComment(t, out, "test-create")
}

func TestRunCreate_rsa(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "id_rsa")

	if err := runCreate("rsa", 2048, "rsa-key", out, false); err != nil {
		t.Fatalf("runCreate: %v", err)
	}

	assertKeyPairExists(t, out, 0o600, 0o644)
}

func TestRunCreate_ecdsa(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "id_ecdsa")

	if err := runCreate("ecdsa", 256, "ecdsa-key", out, false); err != nil {
		t.Fatalf("runCreate: %v", err)
	}

	assertKeyPairExists(t, out, 0o600, 0o644)
}

func TestRunCreate_invalidKeyType(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "id_dsa")
	err := runCreate("dsa", 0, "bad", out, false)
	if err == nil {
		t.Fatal("expected error for unsupported key type")
	}
}

func TestRunCreate_rsaWeakBits(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "id_rsa")
	err := runCreate("rsa", 1024, "x", out, false)
	if err == nil {
		t.Fatal("expected error for weak rsa key size")
	}
}

func TestRunCreate_ecdsaInvalidBits(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "id_ecdsa")
	err := runCreate("ecdsa", 192, "x", out, false)
	if err == nil {
		t.Fatal("expected error for invalid ecdsa curve size")
	}
}

func TestEffectiveBitsForCreate_ecdsaSingleArgDefaultsP256(t *testing.T) {
	cmd := newCreateCommand()
	fb, _ := cmd.Flags().GetInt("bits")
	b, err := effectiveBitsForCreate(cmd, []string{"ecdsa"}, fb)
	if err != nil {
		t.Fatal(err)
	}
	if b != 0 {
		t.Fatalf("want 0 (P256 default when -b omitted), got %d", b)
	}
}

func TestEffectiveBitsForCreate_ecdsaPositional384(t *testing.T) {
	cmd := newCreateCommand()
	fb, _ := cmd.Flags().GetInt("bits")
	b, err := effectiveBitsForCreate(cmd, []string{"ecdsa", "384"}, fb)
	if err != nil {
		t.Fatal(err)
	}
	if b != 384 {
		t.Fatalf("got %d, want 384", b)
	}
}

func TestEffectiveBitsForCreate_ecdsaExplicitFlagOverridesPositional(t *testing.T) {
	cmd := newCreateCommand()
	if err := cmd.ParseFlags([]string{"-b", "521"}); err != nil {
		t.Fatal(err)
	}
	fb, _ := cmd.Flags().GetInt("bits")
	b, err := effectiveBitsForCreate(cmd, []string{"ecdsa", "256"}, fb)
	if err != nil {
		t.Fatal(err)
	}
	if b != 521 {
		t.Fatalf("flag should win: got %d, want 521", b)
	}
}

func TestEffectiveBitsForCreate_positionalRsa(t *testing.T) {
	cmd := newCreateCommand()
	fb, _ := cmd.Flags().GetInt("bits")
	b, err := effectiveBitsForCreate(cmd, []string{"rsa", "2048"}, fb)
	if err != nil {
		t.Fatal(err)
	}
	if b != 2048 {
		t.Fatalf("got %d, want 2048", b)
	}
}

func TestEffectiveBitsForCreate_singleArgUsesFlagDefault(t *testing.T) {
	cmd := newCreateCommand()
	fb, _ := cmd.Flags().GetInt("bits")
	b, err := effectiveBitsForCreate(cmd, []string{"rsa"}, fb)
	if err != nil || b != 4096 {
		t.Fatalf("b=%d err=%v", b, err)
	}
}

func TestEffectiveBitsForCreate_explicitBitsFlagOverridesPositional(t *testing.T) {
	cmd := newCreateCommand()
	if err := cmd.ParseFlags([]string{"-b", "2048"}); err != nil {
		t.Fatal(err)
	}
	fb, _ := cmd.Flags().GetInt("bits")
	b, err := effectiveBitsForCreate(cmd, []string{"rsa", "4096"}, fb)
	if err != nil {
		t.Fatal(err)
	}
	if b != 2048 {
		t.Fatalf("flag should win: got %d, want 2048", b)
	}
}

func TestEffectiveBitsForCreate_ed25519RejectsSecondArg(t *testing.T) {
	cmd := newCreateCommand()
	_, err := effectiveBitsForCreate(cmd, []string{"ed25519", "256"}, 0)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateCommand_positionalRsaWeakRejects(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "rsa")
	cmd := newCreateCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"rsa", "1000", "-o", out, "--force"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for weak rsa bits from positional")
	}
}

func TestCreateCommand_positionalRsa2048(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "rsa")
	cmd := newCreateCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"rsa", "2048", "-o", out})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	assertKeyPairExists(t, out, 0o600, 0o644)
}

func TestCreateCommand_ecdsaNoBitsUsesP256(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "id_ecdsa")
	cmd := newCreateCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"ecdsa", "-o", out})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	assertKeyPairExists(t, out, 0o600, 0o644)
}

func TestCreateCommand_positionalEcdsa384(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "k")
	cmd := newCreateCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"ecdsa", "384", "-o", out})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	assertKeyPairExists(t, out, 0o600, 0o644)
}

func TestCreateCommand_positionalEcdsaInvalidRejects(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "k")
	cmd := newCreateCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"ecdsa", "192", "-o", out, "--force"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid ecdsa curve from positional")
	}
}

func TestRunCreate_existingFileNoForce(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "id_ed25519")
	os.WriteFile(out, []byte("existing"), 0o600)

	err := runCreate("ed25519", 0, "test", out, false)
	if err == nil {
		t.Fatal("expected error when file exists without --force")
	}
}

func TestRunCreate_existingFileWithForce(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "id_ed25519")
	os.WriteFile(out, []byte("existing"), 0o600)

	if err := runCreate("ed25519", 0, "forced", out, true); err != nil {
		t.Fatalf("runCreate with --force: %v", err)
	}

	assertKeyPairExists(t, out, 0o600, 0o644)
}

// --- assertion helpers ---

func assertKeyPairExists(t *testing.T, privPath string, privPerm, pubPerm os.FileMode) {
	t.Helper()
	info, err := os.Stat(privPath)
	if err != nil {
		t.Fatalf("private key not found: %v", err)
	}
	if perm := info.Mode().Perm(); perm != privPerm {
		t.Errorf("private key permissions: got %o, want %o", perm, privPerm)
	}

	pubPath := privPath + ".pub"
	info, err = os.Stat(pubPath)
	if err != nil {
		t.Fatalf("public key not found: %v", err)
	}
	if perm := info.Mode().Perm(); perm != pubPerm {
		t.Errorf("public key permissions: got %o, want %o", perm, pubPerm)
	}
}

func assertKeyComment(t *testing.T, privPath, wantComment string) {
	t.Helper()
	data, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatal(err)
	}
	rawKey, err := ssh.ParseRawPrivateKey(data)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(rawKey, "")
	_ = block

	pubData, err := os.ReadFile(privPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	pubLine := string(pubData)
	if len(pubLine) == 0 {
		t.Fatal("empty public key file")
	}
	if !contains(pubLine, wantComment) {
		t.Errorf("public key line should contain comment %q: %s", wantComment, pubLine)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
