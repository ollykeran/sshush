//go:build !windows

package tray

import (
	"context"
	"crypto/ed25519"
	"net"
	"path/filepath"
	"testing"
	"time"

	sshagent "golang.org/x/crypto/ssh/agent"
)

// serveKeyring serves an in-memory keyring on a Unix socket and returns its
// path. The keyring speaks no sshush-op extension, like somebody else's agent.
func serveKeyring(t *testing.T, kr sshagent.Agent) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_ = sshagent.ServeAgent(kr, c)
			}()
		}
	}()
	return path
}

func TestProbeUnreachable(t *testing.T) {
	if snap := Probe(filepath.Join(t.TempDir(), "nothing.sock")); snap.Err == nil {
		t.Error("want an error for a socket nobody listens on")
	}
}

func TestProbeListsKeyringKeys(t *testing.T) {
	kr := sshagent.NewKeyring()
	_, priv, _ := ed25519.GenerateKey(nil)
	if err := kr.Add(sshagent.AddedKey{PrivateKey: priv, Comment: "probe"}); err != nil {
		t.Fatal(err)
	}
	snap := Probe(serveKeyring(t, kr))
	if snap.Err != nil {
		t.Fatalf("probe: %v", snap.Err)
	}
	if snap.Backend.Mode != "keys" || len(snap.Keys) != 1 || snap.Keys[0].Comment != "probe" {
		t.Errorf("got %+v", snap)
	}
}

func TestPollSendsOnlyChanges(t *testing.T) {
	kr := sshagent.NewKeyring()
	path := serveKeyring(t, kr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	kick := make(chan struct{})
	out := make(chan View)
	go Poll(ctx, path, true, time.Hour, kick, out)

	if v := <-out; v.Status != "No keys loaded" {
		t.Fatalf("first view: got %+v", v)
	}
	_, priv, _ := ed25519.GenerateKey(nil)
	if err := kr.Add(sshagent.AddedKey{PrivateKey: priv, Comment: "added"}); err != nil {
		t.Fatal(err)
	}
	kick <- struct{}{}
	select {
	case v := <-out:
		if len(v.Keys) != 1 {
			t.Errorf("after add: got %+v", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no view after a change and a kick")
	}
	kick <- struct{}{} // nothing changed: Poll must not send, so this kick is taken
	select {
	case v := <-out:
		t.Errorf("unchanged agent sent a view: %+v", v)
	case <-time.After(200 * time.Millisecond):
	}
}
