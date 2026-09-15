package version

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime/debug"
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

// withBuild sets how this package believes the binary was built, for one test.
func withBuild(t *testing.T, version string, buildInfo bool, m debug.Module) {
	t.Helper()
	origVersion, origFromBuildInfo, origModule := Version, fromBuildInfo, module
	Version, fromBuildInfo, module = version, buildInfo, m
	t.Cleanup(func() { Version, fromBuildInfo, module = origVersion, origFromBuildInfo, origModule })
}

// withSumDB points the checksum database lookup at a test server answering with
// status and body, and returns the path of the last request it saw.
func withSumDB(t *testing.T, status int, body string) *string {
	t.Helper()
	var requested string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = r.URL.Path
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	orig := SumDBLookupURL
	SumDBLookupURL = server.URL + "/lookup/"
	t.Cleanup(func() { SumDBLookupURL = orig })
	return &requested
}

// sumDBLookup is a lookup response shaped like sum.golang.org's, go.mod record
// first so a parser that takes the first h1: line gets the wrong one.
const sumDBLookup = `63096182
github.com/ollykeran/sshush v0.1.0/go.mod h1:Oe5UdpFXWdv1OlxFMWveNdYrpF03onYvdg1kJzTsfqA=
github.com/ollykeran/sshush v0.1.0 h1:YtTcclbbnRoZXQzAk69OTl3d8yhPbz+nt9NTz+fcnGc=

go.sum database tree
63096183
`

var goInstalled = debug.Module{
	Path:    "github.com/ollykeran/sshush",
	Version: "v0.1.0",
	Sum:     "h1:YtTcclbbnRoZXQzAk69OTl3d8yhPbz+nt9NTz+fcnGc=",
}

func TestVerifyBinaryChecksum_devBuild(t *testing.T) {
	withBuild(t, "dev", false, debug.Module{})

	result, err := VerifyBinaryChecksum()
	if err != nil {
		t.Fatalf("VerifyBinaryChecksum: %v", err)
	}
	if result.Verified || !strings.Contains(result.Message, "development build") {
		t.Errorf("dev build: got %+v, want unverified with a development-build message", result)
	}
}

// A build inside the repo takes its version from build info but has no module
// source hash, so there is nothing published to check it against.
func TestVerifyBinaryChecksum_sourceBuild(t *testing.T) {
	withBuild(t, "0.1.1-0.20260913120000-abcdef123456+dirty", true, debug.Module{
		Path:    "github.com/ollykeran/sshush",
		Version: "v0.1.1-0.20260913120000-abcdef123456+dirty",
	})

	result, err := VerifyBinaryChecksum()
	if err != nil {
		t.Fatalf("VerifyBinaryChecksum: %v", err)
	}
	if result.Verified || !strings.Contains(result.Message, "built from source") {
		t.Errorf("source build: got %+v, want unverified with a built-from-source message", result)
	}
}

func TestVerifyBinaryChecksum_goInstallMatchesSumDB(t *testing.T) {
	withBuild(t, "0.1.0", true, goInstalled)
	requested := withSumDB(t, http.StatusOK, sumDBLookup)

	result, err := VerifyBinaryChecksum()
	if err != nil {
		t.Fatalf("VerifyBinaryChecksum: %v", err)
	}
	if !result.Verified || !strings.Contains(result.Message, "go install") {
		t.Errorf("go install build: got %+v, want verified with a go-install message", result)
	}
	if want := "/lookup/github.com/ollykeran/sshush@v0.1.0"; *requested != want {
		t.Errorf("looked up %q, want %q", *requested, want)
	}
}

func TestVerifyBinaryChecksum_goInstallDiffersFromSumDB(t *testing.T) {
	tampered := goInstalled
	tampered.Sum = "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	withBuild(t, "0.1.0", true, tampered)
	withSumDB(t, http.StatusOK, sumDBLookup)

	result, err := VerifyBinaryChecksum()
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("got %+v, %v; want a does-not-match error", result, err)
	}
	if result.Verified {
		t.Error("a mismatched source hash was reported as verified")
	}
}

func TestVerifyBinaryChecksum_goInstallUnknownToSumDB(t *testing.T) {
	withBuild(t, "0.1.0", true, goInstalled)
	withSumDB(t, http.StatusNotFound, "not found: unrecognized import path")

	if _, err := VerifyBinaryChecksum(); err == nil {
		t.Fatal("expected an error when the checksum database has no record")
	}
}

func TestModuleSumFromLookup_skipsGoModRecord(t *testing.T) {
	t.Parallel()
	got, err := moduleSumFromLookup(strings.NewReader(sumDBLookup), goInstalled.Path, goInstalled.Version)
	if err != nil {
		t.Fatalf("moduleSumFromLookup: %v", err)
	}
	if got != goInstalled.Sum {
		t.Errorf("got %q, want the module record's %q", got, goInstalled.Sum)
	}
	if _, err := moduleSumFromLookup(strings.NewReader(sumDBLookup), goInstalled.Path, "v9.9.9"); err == nil {
		t.Error("expected an error for a version the response has no record of")
	}
}

func TestEscapeModulePath(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"github.com/ollykeran/sshush": "github.com/ollykeran/sshush",
		"github.com/Azure/azure-sdk":  "github.com/!azure/azure-sdk",
		"example.com/ABc":             "example.com/!a!bc",
	} {
		if got := escapeModulePath(in); got != want {
			t.Errorf("escapeModulePath(%q) = %q, want %q", in, got, want)
		}
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
