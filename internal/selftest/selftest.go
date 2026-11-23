// Package selftest checks that ssh can use the agent: SSH_AUTH_SOCK points at
// it, it answers, it lists keys, and it signs. The checks return results, not
// output, so the CLI and the tray each render them their own way.
package selftest

import (
	"fmt"
	"os"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/transport"
)

// Status is how one check came out.
type Status int

// Run stops after a failed check that later checks depend on.
const (
	OK Status = iota
	Warn
	Fail
)

// Check is one line of the report.
type Check struct {
	Name   string // "env", "agent", "state", "socket", "list" or "sign"
	Detail string
	Status Status
	// Hint, when not empty, says what to do about a failure.
	Hint string
}

// Report is the outcome of Run.
type Report struct {
	Checks []Check
}

// Worst is the most severe status among the checks.
func (r Report) Worst() Status {
	worst := OK
	for _, c := range r.Checks {
		worst = max(worst, c.Status)
	}
	return worst
}

// Run checks the agent at socketPath. authSock is the SSH_AUTH_SOCK ssh will
// see, and persisted the value new processes get when the system keeps one
// (Windows; see platform.PersistedEnv).
func Run(socketPath, authSock, persisted string) Report {
	var r Report
	add := func(name, detail string, s Status) {
		r.Checks = append(r.Checks, Check{Name: name, Detail: detail, Status: s})
	}

	envLine, envOK := authSockEnvLine(authSock, persisted, socketPath)
	if envOK {
		add("env", envLine, OK)
	} else {
		add("env", envLine, Fail)
	}

	session, sessionErr := agent.Open(socketPath)
	if sessionErr == nil {
		defer session.Close()
	}
	var backend agent.Backend
	liveOK := false
	if sessionErr == nil {
		if b, err := session.Backend(); err == nil {
			backend, liveOK = b, true
		}
	}
	if liveOK {
		add("agent", backend.Mode+"  ✓", OK)
	} else {
		add("agent", "unreachable", Fail)
	}

	if backend.Mode == "vault" {
		if backend.VaultLocked {
			add("state", "locked", Fail)
		} else {
			add("state", "unlocked  ✓", OK)
		}
	} else if liveOK {
		add("state", "ready  ✓", OK)
	}

	// A named pipe is not a file to stat: it exists exactly when it can be dialled.
	socketMissing := sessionErr != nil
	if !transport.IsPipe(socketPath) {
		_, statErr := os.Stat(socketPath)
		socketMissing = statErr != nil
	}
	if socketMissing {
		r.Checks = append(r.Checks, Check{Name: "socket", Detail: socketPath + "  ✗", Status: Fail,
			Hint: "agent is not running (try sshush start)"})
		return r
	}
	add("socket", socketPath+"  ✓", OK)

	// The socket file can exist while the dial failed (a stale socket), so the
	// os.Stat check above does not guarantee a session.
	if sessionErr != nil {
		add("list", fmt.Sprintf("✗ %v", sessionErr), Fail)
		return r
	}
	keys, err := session.List()
	if err != nil {
		add("list", fmt.Sprintf("✗ %v", err), Fail)
		return r
	}
	if len(keys) == 0 {
		add("list", "no keys loaded", Warn)
		return r
	}
	add("list", fmt.Sprintf("%d key(s) loaded  ✓", len(keys)), OK)

	if _, err := session.Sign(keys[0], []byte("sshush-test")); err != nil {
		add("sign", fmt.Sprintf("✗ %v", err), Fail)
		return r
	}
	add("sign", keys[0].Comment+"  ✓", OK)
	return r
}

// authSockEnvLine describes what SSH_AUTH_SOCK holds in this process against the
// socket the agent is on, and whether that is what ssh needs to find the agent.
// persisted is the value new processes will get when the system keeps one,
// which explains the commonest surprise on Windows: the variable was just set
// for the user, and this window predates it.
func authSockEnvLine(authSock, persisted, socketPath string) (line string, ok bool) {
	switch {
	case authSock == socketPath:
		return fmt.Sprintf("SSH_AUTH_SOCK=%s  ✓", authSock), true
	case authSock != "":
		return fmt.Sprintf("SSH_AUTH_SOCK=%s (differs from socket)", authSock), false
	case persisted == socketPath:
		return "SSH_AUTH_SOCK not set in this window; new windows have it", false
	case persisted != "":
		return fmt.Sprintf("SSH_AUTH_SOCK not set in this window; new windows get %s (differs from socket)", persisted), false
	default:
		return "SSH_AUTH_SOCK not set", false
	}
}
