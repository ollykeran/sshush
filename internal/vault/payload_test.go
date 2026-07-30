package vault

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildAddKeyOptsPayload_autoloadByte(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "k")
	if err := os.WriteFile(p, []byte("PRIVATE-DATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		autoload bool
		wantTail byte
	}{
		{autoload: true, wantTail: 1},
		{autoload: false, wantTail: 0},
	} {
		payload, err := BuildAddKeyOptsPayload(p, tc.autoload)
		if err != nil {
			t.Fatalf("autoload=%v: %v", tc.autoload, err)
		}
		// v1 format: [1-byte version][4-byte PEM len][PEM][1-byte autoload][4-byte fp len][filepath]
		if payload[0] != 1 {
			t.Fatalf("version byte: got %d want 1", payload[0])
		}
		pemLen := int(binary.BigEndian.Uint32(payload[1:5]))
		if got := payload[5+pemLen]; got != tc.wantTail {
			t.Fatalf("autoload=%v: tail byte got %d want %d", tc.autoload, got, tc.wantTail)
		}
	}
}

func TestBuildAddKeyOptsPayload_filepathRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "id_ed25519")
	keyData := []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nfake-key-data\n-----END OPENSSH PRIVATE KEY-----")
	if err := os.WriteFile(p, keyData, 0o600); err != nil {
		t.Fatal(err)
	}

	payload, err := BuildAddKeyOptsPayload(p, true)
	if err != nil {
		t.Fatal(err)
	}

	// Parse the v1 payload
	if payload[0] != 1 {
		t.Fatalf("version byte: got %d want 1", payload[0])
	}
	pemLen := int(binary.BigEndian.Uint32(payload[1:5]))
	pemData := payload[5 : 5+pemLen]
	if string(pemData) != string(keyData) {
		t.Fatalf("PEM data mismatch: got %q, want %q", pemData, keyData)
	}
	autoloadByte := payload[5+pemLen]
	if autoloadByte != 1 {
		t.Fatalf("autoload byte: got %d, want 1", autoloadByte)
	}
	fpOffset := 5 + pemLen + 1
	fpLen := int(binary.BigEndian.Uint32(payload[fpOffset : fpOffset+4]))
	filepath := string(payload[fpOffset+4 : fpOffset+4+fpLen])
	if filepath != p {
		t.Fatalf("filepath in payload: got %q, want %q", filepath, p)
	}
}
