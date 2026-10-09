package readypipe

import (
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
