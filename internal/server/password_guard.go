package server

import (
	"context"
	"net"
	"sync"
	"time"
)

const (
	// maxConcurrentPasswordChecks caps the password checks running at once. A vault
	// passphrase check is an Argon2id derivation holding 64 MiB, done for a client
	// that has not authenticated, so without a cap a flood of attempts would exhaust
	// the host's memory for free.
	maxConcurrentPasswordChecks = 2

	// passwordFailureDelay is added to every rejected password, so guessing costs
	// the guesser time as well as the server.
	passwordFailureDelay = time.Second

	// passwordLockoutFailures failures from one address, each within
	// passwordLockout of the last, shut that address out of password
	// authentication until passwordLockout has passed since the last of them.
	passwordLockoutFailures = 5
	passwordLockout         = time.Minute
)

// passwordGuard puts the defences a password typed over a network needs, and a
// local unlock prompt does not, in front of a PasswordSource: a cap on concurrent
// checks, a delay on every failure, and a per-address lockout.
type passwordGuard struct {
	source PasswordSource
	slots  chan struct{}

	failureDelay    time.Duration
	lockoutFailures int
	lockout         time.Duration
	now             func() time.Time

	mu       sync.Mutex
	failures map[string]addressFailures
}

// addressFailures is one address's run of failed passwords.
type addressFailures struct {
	count int
	last  time.Time
}

func newPasswordGuard(source PasswordSource) *passwordGuard {
	return &passwordGuard{
		source:          source,
		slots:           make(chan struct{}, maxConcurrentPasswordChecks),
		failureDelay:    passwordFailureDelay,
		lockoutFailures: passwordLockoutFailures,
		lockout:         passwordLockout,
		now:             time.Now,
		failures:        make(map[string]addressFailures),
	}
}

// check reports whether password is accepted from a client at addr. A locked-out
// address is refused without its password being checked at all, even a correct
// one, or the lockout would still confirm a right guess. The client going away
// (ctx ending) while a check waits its turn refuses it.
func (g *passwordGuard) check(ctx context.Context, addr net.Addr, password []byte) bool {
	ok, _, _ := g.verify(ctx, addr, password)
	return ok
}

// Reasons verify gives for refusing a password without it being checked.
const (
	refusedLockedOut  = "address locked out"
	refusedClientLeft = "client left while waiting its turn"
)

// verify is check, saying more about a refusal: why the password never reached
// the source (refusedLockedOut or refusedClientLeft), or, for one that was checked
// and wrong, whether that failure is the one that locked the address out.
func (g *passwordGuard) verify(ctx context.Context, addr net.Addr, password []byte) (ok bool, refusal string, lockedOutNow bool) {
	host := addressHost(addr)
	if g.lockedOut(host) {
		g.pause(ctx)
		return false, refusedLockedOut, false
	}

	select {
	case g.slots <- struct{}{}:
	case <-ctx.Done():
		return false, refusedClientLeft, false
	}
	ok = g.source.Verify(password)
	<-g.slots

	if ok {
		g.forget(host)
		return true, "", false
	}
	lockedOutNow = g.recordFailure(host)
	g.pause(ctx)
	return false, "", lockedOutNow
}

// lockedOut reports whether host has failed often enough, recently enough, to be
// refused outright.
func (g *passwordGuard) lockedOut(host string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	f, ok := g.failures[host]
	return ok && f.count >= g.lockoutFailures && g.now().Sub(f.last) < g.lockout
}

// recordFailure counts a failed password against host, reporting whether this
// failure is the one that locks host out. Runs that have gone quiet for a
// lockout's length are forgotten here too, which keeps the map from growing with
// every address that ever got a password wrong.
func (g *passwordGuard) recordFailure(host string) (nowLockedOut bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	for h, f := range g.failures {
		if now.Sub(f.last) >= g.lockout {
			delete(g.failures, h)
		}
	}
	f := g.failures[host]
	f.count++
	f.last = now
	g.failures[host] = f
	return f.count == g.lockoutFailures
}

// forget clears host's failures after it authenticates.
func (g *passwordGuard) forget(host string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.failures, host)
}

// pause sits out the failure delay, or less if the client leaves first.
func (g *passwordGuard) pause(ctx context.Context) {
	if g.failureDelay <= 0 {
		return
	}
	timer := time.NewTimer(g.failureDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// addressHost is the host part of addr, which is what a lockout is keyed on: a
// client reconnecting from a new source port is still the same client.
func addressHost(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr.String()); err == nil {
		return host
	}
	return addr.String()
}
