package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/version"
)

func TestRunVersion(t *testing.T) {
	t.Parallel()
	cmd := newVersionCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, version.Version) {
		t.Errorf("output missing version %q: %s", version.Version, out)
	}
	if !strings.Contains(out, "sshush") {
		t.Errorf("output missing name %q: %s", "sshush", out)
	}
}

func TestVersionCommand_rejectsArgs(t *testing.T) {
	t.Parallel()
	cmd := newVersionCommand()
	cmd.SetArgs([]string{"extra"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for extra args")
	}
}

func TestVersionCommand_checkUpToDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name": "v0.0.8"}`))
	}))
	defer server.Close()

	origVersion := version.Version
	version.Version = "0.0.8"
	defer func() { version.Version = origVersion }()

	origURL := version.LatestReleaseURL
	version.LatestReleaseURL = server.URL
	defer func() { version.LatestReleaseURL = origURL }()

	cmd := newVersionCommand()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"--check"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version --check: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, version.Version) {
		t.Errorf("output missing version %q: %s", version.Version, out)
	}
	errOut := errBuf.String()
	if !strings.Contains(errOut, "up to date") {
		t.Errorf("expected up-to-date message on stderr, got: %s", errOut)
	}
}

func TestVersionCommand_checkNewerAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name": "v0.1.0"}`))
	}))
	defer server.Close()

	origVersion := version.Version
	version.Version = "0.0.8"
	defer func() { version.Version = origVersion }()

	origURL := version.LatestReleaseURL
	version.LatestReleaseURL = server.URL
	defer func() { version.LatestReleaseURL = origURL }()

	cmd := newVersionCommand()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"--check"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version --check: %v", err)
	}
	errOut := errBuf.String()
	if !strings.Contains(errOut, "v0.1.0") {
		t.Errorf("expected new-version message about v0.1.0 on stderr, got: %s", errOut)
	}
}

func TestVersionCommand_checkNetworkError(t *testing.T) {
	origVersion := version.Version
	version.Version = "0.0.8"
	defer func() { version.Version = origVersion }()

	origURL := version.LatestReleaseURL
	version.LatestReleaseURL = "http://127.0.0.1:1/nonexistent"
	defer func() { version.LatestReleaseURL = origURL }()

	cmd := newVersionCommand()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"--check"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version --check: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "sshush") {
		t.Errorf("output missing name: %s", out)
	}
	errOut := errBuf.String()
	if !strings.Contains(errOut, "Version check failed") {
		t.Errorf("expected error message on stderr for network failure, got: %s", errOut)
	}
}
