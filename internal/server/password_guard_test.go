package server

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

// fakePasswords accepts one password and counts how often it is asked.
type fakePasswords struct {
	accept string

	mu    sync.Mutex
	calls int
}

func (f *fakePasswords) Verify(password []byte) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return string(password) == f.accept
}

func (f *fakePasswords) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// blockingPasswords holds every check until release closes, recording the most
// that were ever in flight together. It accepts nothing.
type blockingPasswords struct {
	release chan struct{}

	mu       sync.Mutex
	inFlight int
	maxSeen  int
	calls    int
}

func (b *blockingPasswords) Verify([]byte) bool {
	b.mu.Lock()
	b.calls++
	b.inFlight++
	if b.inFlight > b.maxSeen {
		b.maxSeen = b.inFlight
	}
	b.mu.Unlock()

	<-b.release

	b.mu.Lock()
	b.inFlight--
	b.mu.Unlock()
	return false
}

func (b *blockingPasswords) snapshot() (inFlight, maxSeen, calls int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.inFlight, b.maxSeen, b.calls
}

// testGuard is a guard over source with no failure delay, on a clock the test
// moves by assigning through the returned pointer.
func testGuard(source PasswordSource) (*passwordGuard, *time.Time) {
	g := newPasswordGuard(source)
	g.failureDelay = 0
	clock := time.Unix(1_700_000_000, 0)
	g.now = func() time.Time { return clock }
	return g, &clock
}

func clientAt(host string) net.Addr {
	return &net.TCPAddr{IP: net.ParseIP(host), Port: 50022}
}

// lockOut fails enough passwords from addr to lock it out.
func lockOut(g *passwordGuard, addr net.Addr) {
	for i := 0; i < passwordLockoutFailures; i++ {
		g.check(context.Background(), addr, []byte("wrong"))
	}
}

func TestPasswordGuard_AcceptsWhatTheSourceAccepts(t *testing.T) {
	g, _ := testGuard(&fakePasswords{accept: "right"})
	ctx := context.Background()
	if !g.check(ctx, clientAt("192.0.2.1"), []byte("right")) {
		t.Error("the right password was refused")
	}
	if g.check(ctx, clientAt("192.0.2.1"), []byte("wrong")) {
		t.Error("a wrong password was accepted")
	}
}

func TestPasswordGuard_LocksOutAnAddressAfterRepeatedFailures(t *testing.T) {
	source := &fakePasswords{accept: "right"}
	g, _ := testGuard(source)
	attacker := clientAt("203.0.113.5")

	lockOut(g, attacker)
	calls := source.callCount()

	if g.check(context.Background(), attacker, []byte("right")) {
		t.Error("a locked-out address got in with the right password")
	}
	if source.callCount() != calls {
		t.Error("a locked-out address still had its password checked")
	}
}

func TestPasswordGuard_LockoutEndsOnceItsTimeHasPassed(t *testing.T) {
	g, clock := testGuard(&fakePasswords{accept: "right"})
	attacker := clientAt("203.0.113.5")

	lockOut(g, attacker)
	*clock = clock.Add(passwordLockout - time.Second)
	if g.check(context.Background(), attacker, []byte("right")) {
		t.Fatal("the lockout ended early")
	}
	*clock = clock.Add(time.Second)
	if !g.check(context.Background(), attacker, []byte("right")) {
		t.Error("the right password was still refused after the lockout")
	}
}

// TestPasswordGuard_LockoutIsKeyedOnTheHostNotThePort checks a client cannot
// dodge a lockout by reconnecting, while other clients are unaffected by it.
func TestPasswordGuard_LockoutIsKeyedOnTheHostNotThePort(t *testing.T) {
	g, _ := testGuard(&fakePasswords{accept: "right"})
	lockOut(g, clientAt("203.0.113.5"))

	reconnected := &net.TCPAddr{IP: net.ParseIP("203.0.113.5"), Port: 60000}
	if g.check(context.Background(), reconnected, []byte("right")) {
		t.Error("a new source port escaped the lockout")
	}
	if !g.check(context.Background(), clientAt("192.0.2.1"), []byte("right")) {
		t.Error("another address was caught by someone else's lockout")
	}
}

func TestPasswordGuard_SuccessClearsEarlierFailures(t *testing.T) {
	g, _ := testGuard(&fakePasswords{accept: "right"})
	ctx := context.Background()
	client := clientAt("192.0.2.1")

	for round := 0; round < 2; round++ {
		for i := 0; i < passwordLockoutFailures-1; i++ {
			g.check(ctx, client, []byte("typo"))
		}
		if !g.check(ctx, client, []byte("right")) {
			t.Fatalf("round %d: the right password was refused after %d typos", round, passwordLockoutFailures-1)
		}
	}
}

func TestPasswordGuard_CapsConcurrentChecks(t *testing.T) {
	source := &blockingPasswords{release: make(chan struct{})}
	g, _ := testGuard(source)

	var wg sync.WaitGroup
	for i := 0; i < maxConcurrentPasswordChecks+3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			g.check(context.Background(), &net.TCPAddr{IP: net.IPv4(192, 0, 2, byte(i+1))}, []byte("x"))
		}(i)
	}

	waitFor(t, "the cap's worth of checks to start", func() bool {
		inFlight, _, _ := source.snapshot()
		return inFlight == maxConcurrentPasswordChecks
	})
	// Give any check that would break the cap the chance to.
	time.Sleep(50 * time.Millisecond)
	if _, maxSeen, _ := source.snapshot(); maxSeen > maxConcurrentPasswordChecks {
		t.Errorf("%d checks ran at once, want at most %d", maxSeen, maxConcurrentPasswordChecks)
	}

	close(source.release)
	wg.Wait()
	if _, _, calls := source.snapshot(); calls != maxConcurrentPasswordChecks+3 {
		t.Errorf("%d checks ran in the end, want all %d", calls, maxConcurrentPasswordChecks+3)
	}
}

func TestPasswordGuard_RefusesAClientThatLeavesWhileWaiting(t *testing.T) {
	source := &blockingPasswords{release: make(chan struct{})}
	g, _ := testGuard(source)
	defer close(source.release)

	for i := 0; i < maxConcurrentPasswordChecks; i++ {
		go g.check(context.Background(), clientAt("192.0.2.1"), []byte("x"))
	}
	waitFor(t, "every slot to be taken", func() bool {
		inFlight, _, _ := source.snapshot()
		return inFlight == maxConcurrentPasswordChecks
	})

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan bool, 1)
	go func() { result <- g.check(ctx, clientAt("198.51.100.7"), []byte("x")) }()
	cancel()

	select {
	case ok := <-result:
		if ok {
			t.Error("a client that left was accepted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a client that left was kept waiting for a slot")
	}
	if _, _, calls := source.snapshot(); calls != maxConcurrentPasswordChecks {
		t.Errorf("the departed client's password was checked anyway (%d checks)", calls)
	}
}

func TestPasswordGuard_DelaysRejectionsButNotSuccesses(t *testing.T) {
	g := newPasswordGuard(&fakePasswords{accept: "right"})
	g.failureDelay = 200 * time.Millisecond
	ctx := context.Background()

	start := time.Now()
	g.check(ctx, clientAt("192.0.2.1"), []byte("wrong"))
	if elapsed := time.Since(start); elapsed < g.failureDelay {
		t.Errorf("a rejection took %v, want at least %v", elapsed, g.failureDelay)
	}

	start = time.Now()
	g.check(ctx, clientAt("192.0.2.1"), []byte("right"))
	if elapsed := time.Since(start); elapsed >= g.failureDelay {
		t.Errorf("a success took %v, want no failure delay", elapsed)
	}
}

func TestAddressHost_DropsThePort(t *testing.T) {
	cases := map[string]net.Addr{
		"203.0.113.5": &net.TCPAddr{IP: net.ParseIP("203.0.113.5"), Port: 1},
		"::1":         &net.TCPAddr{IP: net.ParseIP("::1"), Port: 1},
		"":            nil,
	}
	for want, addr := range cases {
		if got := addressHost(addr); got != want {
			t.Errorf("addressHost(%v) = %q, want %q", addr, got, want)
		}
	}
}

// waitFor polls cond until it holds, failing after a few seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
