package tray

import (
	"testing"
	"time"
)

func TestLockPolicyLocksOn(t *testing.T) {
	p := LockPolicy{OnSessionLock: true}
	if !p.locksOn(EventSessionLock) || p.locksOn(EventSuspend) {
		t.Errorf("%+v", p)
	}
	if DefaultLockPolicy.locksOn(EventSessionLock) || DefaultLockPolicy.locksOn(EventSuspend) || DefaultLockPolicy.idleLimit() != 0 {
		t.Errorf("default %+v", DefaultLockPolicy)
	}
}

func TestLockPolicyRoundTrip(t *testing.T) {
	for _, p := range []LockPolicy{{}, {OnSessionLock: true, OnSuspend: true}, {OnSuspend: true, IdleMinutes: 15}, {OnSessionLock: true, IdleMinutes: 60}} {
		if got := decodeLockPolicy(p.encode()); got != p {
			t.Errorf("%+v came back %+v", p, got)
		}
	}
	for _, b := range [][]byte{nil, {}, {2, 1, 1, 5}, {1, 1}} {
		if got := decodeLockPolicy(b); got != DefaultLockPolicy {
			t.Errorf("%v: got %+v, want the default", b, got)
		}
	}
}

func TestIdleChoices(t *testing.T) {
	want := map[int]string{0: "Never", 5: "After 5 minutes", 60: "After 1 hour", 120: "After 2 hours"}
	for m, label := range want {
		if got := idleLabel(m); got != label {
			t.Errorf("idleLabel(%d) = %q, want %q", m, got, label)
		}
	}
	for m, i := range map[int]int{0: 0, 5: 1, 20: 2, 30: 3, 999: 4} {
		if got := idleIndex(m); got != i {
			t.Errorf("idleIndex(%d) = %d, want %d", m, got, i)
		}
	}
}

func TestIdleWatch(t *testing.T) {
	var w idleWatch
	limit := 15 * time.Minute
	steps := []struct {
		idle time.Duration
		lock bool
	}{
		{time.Minute, false},
		{14 * time.Minute, false},
		{15 * time.Minute, true},  // the limit is reached
		{20 * time.Minute, false}, // still idle: once is enough
		{time.Second, false},      // someone is back
		{16 * time.Minute, true},  // and gone again
	}
	for i, s := range steps {
		if got := w.check(s.idle, limit); got != s.lock {
			t.Errorf("step %d (%v idle): lock %v, want %v", i, s.idle, got, s.lock)
		}
	}
	if w.check(time.Hour, 0) {
		t.Error("locked with idle locking off")
	}
}

func TestAutoLockStatus(t *testing.T) {
	for p, want := range map[LockPolicy]string{
		{}:                                     "The vault now locks only when you lock it.",
		{OnSuspend: true}:                      "The vault now locks when the PC sleeps.",
		{OnSessionLock: true, OnSuspend: true}: "The vault now locks when Windows locks and when the PC sleeps.",
		{OnSessionLock: true, OnSuspend: true, IdleMinutes: 15}: "The vault now locks when Windows locks, when the PC sleeps and after 15 minutes without input.",
		{IdleMinutes: 60}: "The vault now locks after 1 hour without input.",
	} {
		if got := autoLockStatus(p); got != want {
			t.Errorf("%+v: got %q, want %q", p, got, want)
		}
	}
}
