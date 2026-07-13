package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestVaultFile_JSONRoundTrip(t *testing.T) {
	// Build a minimal vault file with base64-sized blobs (encoding/json encodes []byte as base64).
	salt := []byte("0123456789abcdef")
	canary := []byte("canary-ciphertext-here")
	meta := &VaultMetadata{
		Salt:      salt,
		Canary:    canary,
		CreatedAt: "2025-01-01T00:00:00Z",
	}
	identities := []Identity{
		{
			Fingerprint:   "SHA256:abc",
			PublicKey:     []byte("public-key-blob"),
			EncryptedBlob: []byte("encrypted-private-key"),
			Comment:       "test key",
			Autoload:      true,
		},
	}
	f := VaultFile{
		Version:    1,
		Metadata:   meta,
		Identities: identities,
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	var f2 VaultFile
	if err := json.Unmarshal(data, &f2); err != nil {
		t.Fatal(err)
	}
	if f2.Version != f.Version {
		t.Errorf("version: got %d want %d", f2.Version, f.Version)
	}
	if f2.Metadata == nil {
		t.Fatal("metadata is nil after round-trip")
	}
	if string(f2.Metadata.Salt) != string(meta.Salt) {
		t.Errorf("metadata.salt: got %q want %q", f2.Metadata.Salt, meta.Salt)
	}
	if string(f2.Metadata.Canary) != string(meta.Canary) {
		t.Errorf("metadata.canary: got %q want %q", f2.Metadata.Canary, meta.Canary)
	}
	if f2.Metadata.CreatedAt != meta.CreatedAt {
		t.Errorf("metadata.created_at: got %q want %q", f2.Metadata.CreatedAt, meta.CreatedAt)
	}
	if len(f2.Identities) != 1 {
		t.Fatalf("identities: got %d want 1", len(f2.Identities))
	}
	id := f2.Identities[0]
	if id.Fingerprint != identities[0].Fingerprint {
		t.Errorf("identity.fingerprint: got %q want %q", id.Fingerprint, identities[0].Fingerprint)
	}
	if string(id.PublicKey) != string(identities[0].PublicKey) {
		t.Errorf("identity.public_key: got %q want %q", id.PublicKey, identities[0].PublicKey)
	}
	if string(id.EncryptedBlob) != string(identities[0].EncryptedBlob) {
		t.Errorf("identity.encrypted_blob: got %q want %q", id.EncryptedBlob, identities[0].EncryptedBlob)
	}
	if id.Comment != identities[0].Comment {
		t.Errorf("identity.comment: got %q want %q", id.Comment, identities[0].Comment)
	}
	if id.Autoload != identities[0].Autoload {
		t.Errorf("identity.autoload: got %v want %v", id.Autoload, identities[0].Autoload)
	}
}

func TestVaultStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store.SetMetadata(&VaultMetadata{
		Salt:      []byte("salt123456789012"),
		Canary:    []byte("canary1234567890"),
		CreatedAt: "2025-01-02T12:00:00Z",
	})
	store.AddOrReplaceIdentity(Identity{
		Fingerprint:   "SHA256:xyz",
		PublicKey:     []byte("pub"),
		EncryptedBlob: []byte("enc"),
		Comment:       "saved key",
		Autoload:      false,
	})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	// Read raw file and check it's valid JSON with expected content
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f VaultFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal("file is not valid JSON:", err)
	}
	if f.Version != VaultFileVersion {
		t.Errorf("version: got %d want %d", f.Version, VaultFileVersion)
	}
	if f.Metadata == nil {
		t.Fatal("metadata missing in saved file")
	}
	if string(f.Metadata.Salt) != "salt123456789012" {
		t.Errorf("metadata.salt: got %q", f.Metadata.Salt)
	}
	if len(f.Identities) != 1 {
		t.Fatalf("identities: got %d want 1", len(f.Identities))
	}
	if f.Identities[0].Fingerprint != "SHA256:xyz" {
		t.Errorf("identity.fingerprint: got %q", f.Identities[0].Fingerprint)
	}
	// Load again via Open and assert
	store2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	meta := store2.GetMetadata()
	if meta == nil {
		t.Fatal("loaded store has nil metadata")
	}
	if string(meta.Salt) != "salt123456789012" {
		t.Errorf("loaded metadata.salt: got %q", meta.Salt)
	}
	idents := store2.AllIdentities()
	if len(idents) != 1 || idents[0].Fingerprint != "SHA256:xyz" {
		t.Errorf("loaded identities: got %v", idents)
	}
	// File should be 0600
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0077 != 0 {
		t.Errorf("file should not be readable by others: mode %v", info.Mode())
	}
}

func TestVaultStore_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.json")
	// Write initial content via Save()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store.SetMetadata(&VaultMetadata{
		Salt:      []byte("initial_salt_______"),
		Canary:    []byte("initial_canary____"),
		CreatedAt: "2025-01-01T00:00:00Z",
	})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	// Simulate crash: write different content to .tmp only (as if Save() wrote but did not rename)
	tmpPath := path + ".tmp"
	crashFile := VaultFile{
		Version:  VaultFileVersion,
		Metadata: &VaultMetadata{Salt: []byte("crashed_salt________"), Canary: []byte("crashed_canary______"), CreatedAt: "2025-01-03T00:00:00Z"},
		Identities: nil,
	}
	data, _ := json.MarshalIndent(crashFile, "", "  ")
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpPath)
	// Open(path) should read the real path, not .tmp; we should see initial content
	store2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	meta := store2.GetMetadata()
	if meta == nil || string(meta.Salt) != "initial_salt_______" {
		t.Errorf("after simulated crash, expected initial content; got salt %q", string(meta.Salt))
	}
}
