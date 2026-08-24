package readypipe

import (
	"errors"
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

func TestHandshake_Success(t *testing.T) {
	p, c := newPair(t)
	go c.Ready()
	if err := p.Wait(time.Second); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
}

func TestHandshake_Failure(t *testing.T) {
	p, c := newPair(t)
	go c.Fail(errors.New("boom"))
	err := p.Wait(time.Second)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "boom" {
		t.Errorf("expected %q, got %q", "boom", err.Error())
	}
}

func TestHandshake_Timeout(t *testing.T) {
	p, _ := newPair(t)
	start := time.Now()
	err := p.Wait(50 * time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed > time.Second {
		t.Errorf("Wait took too long: %v", elapsed)
	}
}

func TestChild_NilSafe(t *testing.T) {
	var c *Child
	c.Ready()
	c.Fail(errors.New("boom"))
}

func TestParent_DoubleCloseSafe(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	p.CloseWrite()
	p.CloseWrite()
	p.Close()
	p.Close()
}

func TestFromEnv_MissingEnv_ReturnsNil(t *testing.T) {
	t.Setenv(EnvVar, "")
	if c := FromEnv(); c != nil {
		t.Errorf("expected nil Child, got %#v", c)
	}
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
