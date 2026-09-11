package server

import (
	"log/slog"
	"strings"
	"testing"
)

func TestLogHandler_WritesKeyValueRecords(t *testing.T) {
	var out strings.Builder
	slog.New(NewLogHandler(&out, slog.LevelInfo)).Info("auth accepted", "method", "publickey", "user", "kerano")

	if !strings.Contains(out.String(), `level=INFO msg="auth accepted" method=publickey user=kerano`+"\n") {
		t.Errorf("record = %q, want a key=value line", out.String())
	}
}

func TestLogHandler_DropsRecordsBelowItsLevel(t *testing.T) {
	var out strings.Builder
	logger := slog.New(NewLogHandler(&out, slog.LevelInfo))
	logger.Debug("hidden")
	logger.Info("shown")
	if strings.Contains(out.String(), "hidden") || !strings.Contains(out.String(), "shown") {
		t.Errorf("log at info = %q, want only the info record", out.String())
	}
}

// TestLogHandler_KeepsClientTextInItsField checks a client cannot forge a record,
// or a field, through something that gets logged, such as its user name.
func TestLogHandler_KeepsClientTextInItsField(t *testing.T) {
	var out strings.Builder
	forged := "evil\ntime=2026-01-01T00:00:00Z level=INFO msg=\"auth accepted\" user=root"
	slog.New(NewLogHandler(&out, slog.LevelInfo)).Info("auth failed", "user", forged, "remote", "203.0.113.5:40120")

	if n := strings.Count(out.String(), "\n"); n != 1 {
		t.Fatalf("one record became %d lines:\n%s", n, out.String())
	}
	want := `user="evil\ntime=2026-01-01T00:00:00Z level=INFO msg=\"auth accepted\" user=root" remote=203.0.113.5:40120`
	if !strings.Contains(out.String(), want) {
		t.Errorf("record = %q, want the user name quoted in its own field", out.String())
	}
}

func TestParseLogLevel(t *testing.T) {
	cases := map[string]slog.Level{"": slog.LevelInfo, "info": slog.LevelInfo, " DEBUG ": slog.LevelDebug}
	for name, want := range cases {
		got, err := ParseLogLevel(name)
		if err != nil || got != want {
			t.Errorf("ParseLogLevel(%q) = %v, %v; want %v", name, got, err, want)
		}
	}
	if _, err := ParseLogLevel("verbose"); err == nil {
		t.Error(`ParseLogLevel("verbose") succeeded, want an error naming the choices`)
	}
}
