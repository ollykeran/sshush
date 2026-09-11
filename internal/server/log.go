package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// logTimeFormat starts every log line: sortable, to the millisecond, and with the
// zone, so lines still read in order across a DST change.
const logTimeFormat = "2006-01-02T15:04:05.000Z07:00"

// ParseLogLevel reads [server].log_level. Empty means "info", which logs every
// connection, sign-in attempt and session; "debug" adds detail sshd keeps for its
// debug levels, such as the command a session runs.
func ParseLogLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	default:
		return 0, fmt.Errorf("log_level %q: want \"info\" or \"debug\"", name)
	}
}

// NewLogHandler returns a slog.Handler writing one sshd-style line per record:
//
//	2026-09-11T10:22:01.123+01:00 sshushd[4242]: Accepted publickey for kerano from 203.0.113.5 port 50022 ssh2: ED25519 SHA256:…
//
// Records below level are dropped. Debug, warning and error records are marked the
// way sshd marks them ("debug: ", "warning: ", "error: "). Control characters are
// escaped, because much of what gets logged — user names, subsystem names,
// commands — is whatever a client chose to send, and a newline in it must not be
// able to forge a line. Attributes, which the server does not use, are appended
// as key=value.
func NewLogHandler(w io.Writer, level slog.Leveler) slog.Handler {
	return &lineHandler{
		out:    &lockedWriter{w: w},
		level:  level,
		prefix: "sshushd[" + strconv.Itoa(os.Getpid()) + "]: ",
	}
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

type lineHandler struct {
	out    *lockedWriter
	level  slog.Leveler
	prefix string
	attrs  string
}

func (h *lineHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *lineHandler) Handle(_ context.Context, r slog.Record) error {
	t := r.Time
	if t.IsZero() {
		t = time.Now()
	}
	var b strings.Builder
	b.WriteString(t.Format(logTimeFormat))
	b.WriteByte(' ')
	b.WriteString(h.prefix)
	b.WriteString(levelMark(r.Level))
	writeEscaped(&b, r.Message)
	b.WriteString(h.attrs)
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(&b, a)
		return true
	})
	b.WriteByte('\n')

	h.out.mu.Lock()
	defer h.out.mu.Unlock()
	_, err := io.WriteString(h.out.w, b.String())
	return err
}

func (h *lineHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var b strings.Builder
	b.WriteString(h.attrs)
	for _, a := range attrs {
		writeAttr(&b, a)
	}
	clone := *h
	clone.attrs = b.String()
	return &clone
}

func (h *lineHandler) WithGroup(string) slog.Handler { return h }

func writeAttr(b *strings.Builder, a slog.Attr) {
	if a.Equal(slog.Attr{}) {
		return
	}
	b.WriteByte(' ')
	writeEscaped(b, a.Key)
	b.WriteByte('=')
	writeEscaped(b, a.Value.String())
}

func levelMark(level slog.Level) string {
	switch {
	case level < slog.LevelInfo:
		return "debug: "
	case level < slog.LevelWarn:
		return ""
	case level < slog.LevelError:
		return "warning: "
	default:
		return "error: "
	}
}

// writeEscaped writes s with control characters, invalid UTF-8 and the Unicode
// direction overrides escaped, so each record stays one line that reads as it is.
func writeEscaped(b *strings.Builder, s string) {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(b, `\x%02x`, s[i])
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(b, `\x%02x`, r)
		case (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069):
			fmt.Fprintf(b, `\u%04x`, r)
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
}
