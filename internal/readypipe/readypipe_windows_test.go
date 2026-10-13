package readypipe

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// childModeEnv tells a re-run of this test binary which dying daemon to play
// (see TestChildProcess).
const childModeEnv = "SSHUSH_READYPIPE_TEST_CHILD"

// TestChildProcess is not a test of its own: run with childModeEnv set, it is
// the daemon the tests below start, which exits in one of the ways a real one
// can before it is ready.
func TestChildProcess(t *testing.T) {
	switch os.Getenv(childModeEnv) {
	case "":
		t.Skip("only runs as the child of another test")
	case "exit":
		// Dies without ever looking at the pipe.
		os.Exit(7)
	case "fail":
		// Reports why it cannot start, then goes at once.
		FromEnv().Fail(errors.New("boom"))
		os.Exit(1)
	}
}

// startChild starts this test binary as the daemon of p, in the given mode, the
// way sshushd's StartDaemon does.
func startChild(t *testing.T, p *Parent, mode string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestChildProcess$")
	cmd.Env = append(os.Environ(), childModeEnv+"="+mode)
	p.Attach(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p.CloseWrite()
	p.Started(cmd)
}

func newPair(t *testing.T) (*Parent, *Child) {
	t.Helper()
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	t.Setenv(EnvVar, p.name)
	c := FromEnv()
	if c == nil {
		t.Fatal("expected FromEnv to connect to the parent's pipe")
	}
	return p, c
}

func TestFromEnv_InvalidEnv_ReturnsNil(t *testing.T) {
	for _, v := range []string{"not-a-pipe", `\\.\pipe\sshush-ready-nobody-listening`} {
		t.Setenv(EnvVar, v)
		if c := FromEnv(); c != nil {
			t.Errorf("value %q: expected nil Child, got %#v", v, c)
		}
	}
}

// A child that never connects (it died first, say) must not hang the parent.
func TestHandshake_TimeoutWithoutChild(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	start := time.Now()
	if err := p.Wait(50 * time.Millisecond); err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Wait took too long: %v", elapsed)
	}
}

// A child that exits before connecting is known to be gone: the parent must say
// so, with how it went, rather than wait out a timeout that cannot end well.
func TestHandshake_ChildExitsBeforeConnecting(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	startChild(t, p, "exit")

	const timeout = 2 * time.Minute
	start := time.Now()
	err = p.Wait(timeout)
	if err == nil {
		t.Fatal("expected an error for a child that exited without connecting")
	}
	for _, want := range []string{"exited before it was ready", "exit status 7"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	// Generous, for a slow runner starting the child: the point is only that it
	// is nowhere near the timeout.
	if elapsed := time.Since(start); elapsed > timeout/2 {
		t.Errorf("Wait took %v: it sat out the timeout instead of noticing the exit", elapsed)
	}
}

// A child that connects, writes its error and exits has both a connection and
// an exit for the parent to find. Its own words must win, every time.
func TestHandshake_ChildFailsThenExits(t *testing.T) {
	for i := 0; i < 5; i++ {
		p, err := New()
		if err != nil {
			t.Fatal(err)
		}
		startChild(t, p, "fail")
		// Let the child finish first, so the exit is certainly there for Wait
		// to see alongside the connection: the case that could go wrong.
		for deadline := time.Now().Add(time.Minute); len(p.exited) == 0 && time.Now().Before(deadline); {
			time.Sleep(10 * time.Millisecond)
		}
		err = p.Wait(2 * time.Minute)
		p.Close()
		if err == nil || err.Error() != "boom" {
			t.Fatalf("run %d: got %v, want the child's own error %q", i, err, "boom")
		}
	}
}
