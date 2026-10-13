package sshushd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// This test process plays the daemon: it holds the stop event, so requestStop
// from "another process" must cancel its context.
func TestStopRequest_cancelsDaemonContext(t *testing.T) {
	pid := os.Getpid()
	if daemonAlive(pid) {
		t.Fatal("no stop event should exist before withStopRequest")
	}
	ctx, release := withStopRequest(context.Background())
	defer release()
	if !daemonAlive(pid) {
		t.Fatal("expected the stop event to mark this process as a daemon")
	}
	if err := requestStop(pid); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("context was not cancelled by the stop request")
	}
}

func TestWithStopRequest_releaseRemovesEvent(t *testing.T) {
	_, release := withStopRequest(context.Background())
	release()
	if daemonAlive(os.Getpid()) {
		t.Fatal("the stop event must be gone once released")
	}
}

// A live process that is not a daemon — here, this test — must never be
// stopped on the strength of a stale pidfile; the pidfile is cleared instead.
func TestStopDaemon_stalePidfileOfLiveProcess(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "sshush.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	err := StopDaemon(pidPath)
	if !errors.Is(err, errNotDaemon) {
		t.Fatalf("got %v, want errNotDaemon", err)
	}
	if _, statErr := os.Stat(pidPath); !os.IsNotExist(statErr) {
		t.Fatal("expected the stale pidfile to be removed")
	}
	if !processAlive(os.Getpid()) {
		t.Fatal("processAlive must report the running test process")
	}
}
