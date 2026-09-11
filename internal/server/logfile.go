package server

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// DefaultLogMaxBytes is how large the server log grows before it is rotated.
const DefaultLogMaxBytes = 10 << 20

// LogFile is the server's append-only log file, kept to a size. A write that would
// take it past its limit rotates it first: the file becomes path.1, replacing any
// older copy, and a fresh file takes its place. The log so never holds much more
// than twice the limit, and the last rotation's worth of history survives. Writes
// are serialised, so one LogFile can be shared.
type LogFile struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	file     *os.File
	size     int64
}

// OpenLogFile opens path for appending, creating the file (mode 600) and its
// directory (mode 700) if needed: the log records who connected from where.
// maxBytes of zero or less means DefaultLogMaxBytes.
func OpenLogFile(path string, maxBytes int64) (*LogFile, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultLogMaxBytes
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("server: log directory: %w", err)
	}
	l := &LogFile{path: path, maxBytes: maxBytes}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *LogFile) open() error {
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("server: open log %s: %w", l.path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("server: stat log %s: %w", l.path, err)
	}
	l.file, l.size = f, info.Size()
	return nil
}

// Write appends p, rotating first if p would take the file past its limit. A
// single write larger than the limit still lands whole, at the start of a file.
func (l *LogFile) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return 0, os.ErrClosed
	}
	if l.size > 0 && l.size+int64(len(p)) > l.maxBytes {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := l.file.Write(p)
	l.size += int64(n)
	return n, err
}

// rotate moves the current file to path.1 and starts a new one. A rename that
// fails leaves logging carrying on in the old file rather than stopping.
func (l *LogFile) rotate() error {
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("server: close log %s: %w", l.path, err)
	}
	l.file = nil
	_ = os.Rename(l.path, l.path+".1")
	return l.open()
}

// Close closes the file. Writes after Close fail.
func (l *LogFile) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
