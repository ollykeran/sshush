//go:build !windows

package readypipe

import (
	"strconv"
	"testing"
	"time"
)

func newPair(t *testing.T) (*Parent, *Child) {
	t.Helper()
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p, &Child{w: p.w}
}

func TestFromEnv_InvalidEnv_ReturnsNil(t *testing.T) {
	for _, v := range []string{"not-a-number", "-1"} {
		t.Setenv(EnvVar, v)
		if c := FromEnv(); c != nil {
			t.Errorf("value %q: expected nil Child, got %#v", v, c)
		}
	}
}

func TestFromEnv_ValidFD(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	t.Setenv(EnvVar, strconv.Itoa(int(p.w.Fd())))
	c := FromEnv()
	if c == nil {
		t.Fatal("expected non-nil Child")
	}
	go c.Ready()
	if err := p.Wait(time.Second); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
}
