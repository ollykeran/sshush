package version

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCheckLatest_newerAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name": "v0.1.0"}`))
	}))
	defer server.Close()

	origURL := LatestReleaseURL
	LatestReleaseURL = server.URL
	defer func() { LatestReleaseURL = origURL }()

	origVersion := Version
	Version = "0.0.8"
	defer func() { Version = origVersion }()

	msg, err := CheckLatest()
	if err != nil {
		t.Fatalf("CheckLatest: %v", err)
	}
	if msg == "" {
		t.Fatal("expected a new-version message")
	}
	if !strings.Contains(msg, "v0.1.0") {
		t.Errorf("expected message about v0.1.0, got: %s", msg)
	}
}

func TestCheckLatest_upToDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name": "v0.0.8"}`))
	}))
	defer server.Close()

	origURL := LatestReleaseURL
	LatestReleaseURL = server.URL
	defer func() { LatestReleaseURL = origURL }()

	origVersion := Version
	Version = "0.0.8"
	defer func() { Version = origVersion }()

	msg, err := CheckLatest()
	if err != nil {
		t.Fatalf("CheckLatest: %v", err)
	}
	if msg != "" {
		t.Errorf("expected no message for up-to-date, got: %s", msg)
	}
}

func TestCheckLatest_devBuild(t *testing.T) {
	origVersion := Version
	Version = "dev"
	defer func() { Version = origVersion }()

	msg, err := CheckLatest()
	if err != nil {
		t.Fatalf("CheckLatest: %v", err)
	}
	if msg != "" {
		t.Errorf("expected no message for dev build, got: %s", msg)
	}
}

func TestCheckLatest_networkError(t *testing.T) {
	origURL := LatestReleaseURL
	LatestReleaseURL = "http://127.0.0.1:1/nonexistent"
	defer func() { LatestReleaseURL = origURL }()

	origVersion := Version
	Version = "0.0.8"
	defer func() { Version = origVersion }()

	msg, err := CheckLatest()
	if err == nil {
		t.Fatal("expected error for network failure")
	}
	if msg != "" {
		t.Errorf("expected no message on error, got: %s", msg)
	}
}

func TestCompareSemver(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b string
		want int
	}{
		{"0.0.8", "0.1.0", -1},
		{"0.1.0", "0.0.8", 1},
		{"0.0.8", "0.0.8", 0},
		{"0.0.8", "0.0.9", -1},
		{"1.0.0", "0.9.9", 1},
		{"0.0.8", "0.0.8-alpha", 0},
		{"0.0", "0.0.1", -1},
	}
	for _, tt := range tests {
		got := compareSemver(tt.a, tt.b)
		if (got < 0 && tt.want >= 0) || (got > 0 && tt.want <= 0) || (got == 0 && tt.want != 0) {
			t.Errorf("compareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestVerifyChecksum_match(t *testing.T) {
	t.Parallel()
	f := t.TempDir() + "/testbin"
	mustWriteFile(t, f, []byte("hello world"))

	hash := mustFileSHA256(t, f)
	cs := hash + "\n"

	msg, err := verifyChecksum(f, strings.NewReader(cs))
	if err != nil {
		t.Fatalf("verifyChecksum: %v", err)
	}
	if msg != "" {
		t.Errorf("expected no message, got: %s", msg)
	}
}

func TestVerifyChecksum_mismatch(t *testing.T) {
	t.Parallel()
	f := t.TempDir() + "/testbin"
	mustWriteFile(t, f, []byte("hello world"))

	cs := "0000000000000000000000000000000000000000000000000000000000000000\n"

	_, err := verifyChecksum(f, strings.NewReader(cs))
	if err == nil {
		t.Fatal("expected error for checksum mismatch")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("expected 'checksum mismatch' in error, got: %v", err)
	}
}

func TestVerifyChecksum_empty(t *testing.T) {
	t.Parallel()
	f := t.TempDir() + "/testbin"
	mustWriteFile(t, f, []byte("hello world"))

	_, err := verifyChecksum(f, strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error for empty checksum")
	}
}

func TestVerifyChecksum_trimmed(t *testing.T) {
	t.Parallel()
	f := t.TempDir() + "/testbin"
	mustWriteFile(t, f, []byte("hello world"))

	hash := mustFileSHA256(t, f)
	cs := "  " + hash + "  \n"

	msg, err := verifyChecksum(f, strings.NewReader(cs))
	if err != nil {
		t.Fatalf("verifyChecksum: %v", err)
	}
	if msg != "" {
		t.Errorf("expected no message, got: %s", msg)
	}
}

func TestFileSHA256(t *testing.T) {
	t.Parallel()
	f := t.TempDir() + "/sha256test"
	mustWriteFile(t, f, []byte("hello world"))

	h, err := fileSHA256(f)
	if err != nil {
		t.Fatalf("fileSHA256: %v", err)
	}

	expected := sha256.Sum256([]byte("hello world"))
	expectedHex := hex.EncodeToString(expected[:])
	if h != expectedHex {
		t.Errorf("fileSHA256 = %q, want %q", h, expectedHex)
	}
}

func TestVerifyBinaryChecksum_devBuild(t *testing.T) {
	origVersion := Version
	Version = "dev"
	defer func() { Version = origVersion }()

	msg, err := VerifyBinaryChecksum()
	if err != nil {
		t.Fatalf("VerifyBinaryChecksum: %v", err)
	}
	if msg != "" {
		t.Errorf("expected no message for dev build, got: %s", msg)
	}
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func mustFileSHA256(t *testing.T, path string) string {
	t.Helper()
	h, err := fileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
