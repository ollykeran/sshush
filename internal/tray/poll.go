package tray

import (
	"context"
	"time"

	"github.com/ollykeran/sshush/internal/agent"
)

// Probe asks the agent at socketPath what it is and what it holds, on one
// Session. A locked vault is not asked for its keys: it has none to show.
func Probe(socketPath string) Snapshot {
	s, err := agent.Open(socketPath)
	if err != nil {
		return Snapshot{Err: err}
	}
	defer s.Close()
	backend, err := s.Backend()
	if err != nil {
		return Snapshot{Err: err}
	}
	snap := Snapshot{Backend: backend}
	if backend.Mode == "vault" && backend.VaultLocked {
		return snap
	}
	snap.Keys, err = s.List()
	if err != nil {
		return Snapshot{Err: err}
	}
	return snap
}

// Poll probes socketPath every interval, and at once whenever kick receives,
// and sends a View on out only when it differs from the last one sent. managed
// is passed to ViewOf. It returns when ctx is done.
func Poll(ctx context.Context, socketPath string, managed bool, interval time.Duration, kick <-chan struct{}, out chan<- View) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var last View
	sent := false
	for {
		v := ViewOf(Probe(socketPath), managed)
		if !sent || !v.Equal(last) {
			select {
			case out <- v:
				last, sent = v, true
			case <-ctx.Done():
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-kick:
		}
	}
}
