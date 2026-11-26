package tray

import (
	"fmt"
	"strings"
	"time"
)

// The tray locks the vault when the PC is left: when Windows locks, when the
// PC goes to sleep, or after a spell with no keyboard or mouse input. Only
// the tray sees these, so only a vault agent the tray watches is locked; an
// agent-side timer for CLI-only use is a separate matter.

// LockPolicy says when the tray locks the vault by itself.
type LockPolicy struct {
	// OnSessionLock locks when Windows locks, or the session is switched
	// away from.
	OnSessionLock bool
	// OnSuspend locks as the PC goes to sleep or hibernates.
	OnSuspend bool
	// IdleMinutes locks after that long with no input at the PC; 0 never.
	IdleMinutes int
}

// DefaultLockPolicy never locks the vault by itself: it stays unlocked until
// it is locked by hand, or auto-lock is turned on in the window.
var DefaultLockPolicy = LockPolicy{}

// idleChoices are the idle times the window offers, in minutes; 0 is never.
var idleChoices = []int{0, 5, 15, 30, 60}

// LockEvent is something that happened at the PC that may lock the vault.
type LockEvent int

// The events the tray watches for.
const (
	EventSessionLock LockEvent = iota
	EventSuspend
)

// locksOn reports whether p locks the vault on ev.
func (p LockPolicy) locksOn(ev LockEvent) bool {
	switch ev {
	case EventSessionLock:
		return p.OnSessionLock
	case EventSuspend:
		return p.OnSuspend
	}
	return false
}

// idleLimit is how long without input locks the vault, or 0 for never.
func (p LockPolicy) idleLimit() time.Duration {
	return time.Duration(p.IdleMinutes) * time.Minute
}

// encode is p as the bytes the tray stores it as.
func (p LockPolicy) encode() []byte {
	b := []byte{1, 0, 0, byte(min(max(p.IdleMinutes, 0), 255))}
	if p.OnSessionLock {
		b[1] = 1
	}
	if p.OnSuspend {
		b[2] = 1
	}
	return b
}

// decodeLockPolicy reads what encode stored, or the default for anything
// else, such as nothing stored yet.
func decodeLockPolicy(b []byte) LockPolicy {
	if len(b) != 4 || b[0] != 1 {
		return DefaultLockPolicy
	}
	return LockPolicy{OnSessionLock: b[1] == 1, OnSuspend: b[2] == 1, IdleMinutes: int(b[3])}
}

// idleLabel names an idle choice for the drop-down.
func idleLabel(minutes int) string {
	switch {
	case minutes <= 0:
		return "Never"
	case minutes == 60:
		return "After 1 hour"
	case minutes%60 == 0:
		return fmt.Sprintf("After %d hours", minutes/60)
	}
	return fmt.Sprintf("After %d minutes", minutes)
}

// idleIndex is the drop-down position of minutes, choosing the nearest
// choice not longer than it for a value the list does not have.
func idleIndex(minutes int) int {
	best := 0
	for i, m := range idleChoices {
		if m <= minutes {
			best = i
		}
	}
	return best
}

// idleWatch decides when time without input locks the vault: once each time
// the limit is passed. Until input comes again it does not fire again, so a
// vault unlocked from elsewhere while the PC sits idle is not locked again at
// once.
type idleWatch struct{ fired bool }

// check takes how long the PC has had no input and reports whether to lock
// now under limit (0 for never).
func (w *idleWatch) check(idle, limit time.Duration) bool {
	if limit <= 0 || idle < limit {
		w.fired = false
		return false
	}
	if w.fired {
		return false
	}
	w.fired = true
	return true
}

// autoLockStatus says, in a line, when the vault now locks by itself.
func autoLockStatus(p LockPolicy) string {
	var when []string
	if p.OnSessionLock {
		when = append(when, "when Windows locks")
	}
	if p.OnSuspend {
		when = append(when, "when the PC sleeps")
	}
	if p.IdleMinutes > 0 {
		when = append(when, fmt.Sprintf("after %s without input", strings.TrimPrefix(idleLabel(p.IdleMinutes), "After ")))
	}
	switch len(when) {
	case 0:
		return "The vault now locks only when you lock it."
	case 1:
		return "The vault now locks " + when[0] + "."
	}
	return "The vault now locks " + strings.Join(when[:len(when)-1], ", ") + " and " + when[len(when)-1] + "."
}
