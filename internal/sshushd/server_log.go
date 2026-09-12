package sshushd

import (
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

// The server log is rotated once it reaches serverLogMaxMB, keeping the
// serverLogBackups most recent older copies, compressed. However long the server
// runs, the log so takes about 10 MiB plus those copies.
const (
	serverLogMaxMB   = 10
	serverLogBackups = 3
)

// newServerLogWriter returns the rotating writer the server's log goes through.
func newServerLogWriter(path string) *lumberjack.Logger {
	return &lumberjack.Logger{
		Filename:   path,
		MaxSize:    serverLogMaxMB,
		MaxBackups: serverLogBackups,
		Compress:   true,
	}
}

// prepareServerLog makes sure the log at path can be written before the daemon
// detaches: lumberjack only opens the file at the first write, by which time an
// error would have nowhere to go. It creates the directory (mode 700) and the file
// (mode 600) itself, since the log records who connected from where, and
// lumberjack would create a missing directory readable by everyone. Rotated
// copies keep the file's mode.
func prepareServerLog(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}
