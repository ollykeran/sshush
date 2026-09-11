package server

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLogHandler_WritesOneSshdStyleLinePerRecord(t *testing.T) {
	var out strings.Builder
	h := NewLogHandler(&out, slog.LevelInfo)

	at := time.Date(2026, 9, 11, 10, 22, 1, 123_000_000, time.FixedZone("BST", 3600))
	r := slog.NewRecord(at, slog.LevelInfo, "Accepted publickey for kerano from 203.0.113.5 port 50022 ssh2", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	want := "2026-09-11T10:22:01.123+01:00 sshushd[" + strconv.Itoa(os.Getpid()) + "]: " +
		"Accepted publickey for kerano from 203.0.113.5 port 50022 ssh2\n"
	if out.String() != want {
		t.Errorf("line = %q\nwant   %q", out.String(), want)
	}
}

func TestLogHandler_MarksLevelsTheWaySshdDoes(t *testing.T) {
	var out strings.Builder
	logger := slog.New(NewLogHandler(&out, slog.LevelDebug))
	logger.Debug("one")
	logger.Info("two")
	logger.Warn("three")
	logger.Error("four")

	for _, want := range []string{"]: debug: one\n", "]: two\n", "]: warning: three\n", "]: error: four\n"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("log does not contain %q:\n%s", want, out.String())
		}
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

// TestLogHandler_KeepsClientTextOnOneLine checks a client cannot forge a log line
// by putting a newline in something that gets logged, such as its user name.
func TestLogHandler_KeepsClientTextOnOneLine(t *testing.T) {
	var out strings.Builder
	logger := slog.New(NewLogHandler(&out, slog.LevelInfo))
	logger.Info("Failed password for evil\n2026-01-01T00:00:00.000Z sshushd[1]: Accepted password for root\x1b[2K\u202e")

	if strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("one record became %d lines:\n%s", strings.Count(out.String(), "\n"), out.String())
	}
	for _, want := range []string{`evil\x0a2026`, `root\x1b[2K`, `\u202e`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("line %q does not escape to %q", out.String(), want)
		}
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
