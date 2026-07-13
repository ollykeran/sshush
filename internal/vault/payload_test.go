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
		pemLen := int(binary.BigEndian.Uint32(payload[:4]))
		if got := payload[4+pemLen]; got != tc.wantTail {
			t.Fatalf("autoload=%v: tail byte got %d want %d", tc.autoload, got, tc.wantTail)
		}
	}
}
