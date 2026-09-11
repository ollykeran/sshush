package server

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// ParseLogLevel reads [server].log_level. Empty means "info", which logs every
// connection, sign-in attempt and session; "debug" adds detail such as the command
// a session runs.
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

// NewLogHandler returns the handler the server's log is written with: slog's text
// format, one key=value record per line, dropping records below level. Values are
// quoted and escaped where they need to be, so text a client chose — a user name, a
// subsystem, a forwarding destination — can neither break a record across lines
// nor pass for a field of its own.
func NewLogHandler(w io.Writer, level slog.Leveler) slog.Handler {
	return slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
}

// discardLogger stands in for a nil Server.Log.
var discardLogger = slog.New(slog.DiscardHandler)

func (s *Server) logger() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return discardLogger
}
