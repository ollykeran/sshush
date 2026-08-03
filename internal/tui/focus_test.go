package tui

import "testing"

func TestFocusRingSkipEd25519Options(t *testing.T) {
	r := NewFocusRing(6)
	r.SetSkip(func(i int) bool { return i == 1 })
	r.SetIndex(0)
	r.Next()
	if r.Index() != 2 {
		t.Fatalf("next skipped 1: got %d want 2", r.Index())
	}
}

func TestFocusRingPrevAtStart(t *testing.T) {
	r := NewFocusRing(3)
	r.SetIndex(0)
	if !r.Prev() {
		t.Fatal("Prev at start should return true (exit)")
	}
}

func TestFocusRingSetIndexSkips(t *testing.T) {
	r := NewFocusRing(3)
	r.SetSkip(func(i int) bool { return i == 1 })
	r.SetIndex(1)
	if r.Index() == 1 {
		t.Fatal("SetIndex should move off skipped slot")
	}
}
