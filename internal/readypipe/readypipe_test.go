package readypipe

import (
	"errors"
	"testing"
	"time"
)

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
