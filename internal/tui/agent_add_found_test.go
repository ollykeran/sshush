package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/ollykeran/sshush/internal/utils"
)

// addFlowStubs replaces the daemon and agent seams for one test and records calls.
type addFlowStubs struct {
	starts     int
	startErr   error
	running    bool
	addedPaths []string
}

type stubAddedMsg struct{ path string }

func installAddFlowStubs(t *testing.T) *addFlowStubs {
	t.Helper()
	st := &addFlowStubs{}
	origStart, origRunning, origAdd := startDaemon, daemonAlreadyRunning, addKeyToAgent
	t.Cleanup(func() {
		startDaemon, daemonAlreadyRunning, addKeyToAgent = origStart, origRunning, origAdd
	})
	startDaemon = func(_, _ string) error {
		st.starts++
		return st.startErr
	}
	daemonAlreadyRunning = func(string) bool { return st.running }
	addKeyToAgent = func(_, path string) tea.Cmd {
		st.addedPaths = append(st.addedPaths, path)
		return func() tea.Msg { return stubAddedMsg{path: path} }
	}
	return st
}

func newAgentWithFoundKey(path string) *AgentScreen {
	_, agent := newAgentTestSkeleton()
	agent.foundKeys = []utils.KeyPath{{Path: path}}
	agent.loadedFPs = map[string]bool{}
	agent.focus = agentFocusFound
	agent.foundSelected = 0
	return agent
}

func daemonWidgetValue(sk *Skeleton) string {
	for _, w := range sk.widgets {
		if w.id == daemonStatusWidgetID() {
			return w.value
		}
	}
	return ""
}

func TestAgentEnterFoundKeyDaemonRunningAddsDirectly(t *testing.T) {
	st := installAddFlowStubs(t)
	agent := newAgentWithFoundKey("/tmp/id_found")
	agent.daemonRunning = true

	_, cmd := agent.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on found key should return a cmd")
	}
	if got, ok := cmd().(stubAddedMsg); !ok || got.path != "/tmp/id_found" {
		t.Fatalf("cmd msg = %#v, want direct add of /tmp/id_found", got)
	}
	if st.starts != 0 {
		t.Errorf("daemon started %d time(s), want 0 when already running", st.starts)
	}
}

func TestAgentEnterFoundKeyDaemonStoppedStartsThenAdds(t *testing.T) {
	st := installAddFlowStubs(t)
	agent := newAgentWithFoundKey("/tmp/id_found")
	agent.daemonRunning = false
	gen := agent.vaultPollGen

	_, cmd := agent.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on found key should return a cmd")
	}
	if len(st.addedPaths) != 0 {
		t.Fatalf("key added before daemon start: %v", st.addedPaths)
	}
	if agent.status != "starting agent..." || agent.statusErr {
		t.Errorf("status=%q err=%v, want 'starting agent...' without error", agent.status, agent.statusErr)
	}

	msg, ok := cmd().(daemonStartedForAddMsg)
	if !ok {
		t.Fatalf("cmd msg = %T, want daemonStartedForAddMsg", msg)
	}
	if st.starts != 1 {
		t.Fatalf("daemon started %d time(s), want 1", st.starts)
	}
	if msg.err != nil || msg.path != "/tmp/id_found" {
		t.Fatalf("msg = %#v, want success for /tmp/id_found", msg)
	}

	_, cmd = agent.Update(msg)
	if cmd == nil {
		t.Fatal("successful start should return follow-up cmds")
	}
	if !agent.daemonRunning {
		t.Error("daemonRunning should be true after start")
	}
	if got := daemonWidgetValue(agent.sk); got != "running" {
		t.Errorf("daemon widget = %q, want running", got)
	}
	if agent.vaultPollGen != gen+1 {
		t.Errorf("vaultPollGen = %d, want a fresh poll chain (%d)", agent.vaultPollGen, gen+1)
	}
	if len(st.addedPaths) != 1 || st.addedPaths[0] != "/tmp/id_found" {
		t.Errorf("added paths = %v, want [/tmp/id_found]", st.addedPaths)
	}
}

func TestAgentFoundKeyStaleStoppedStateSkipsStart(t *testing.T) {
	st := installAddFlowStubs(t)
	st.running = true // cached state says stopped, but the daemon is actually up
	agent := newAgentWithFoundKey("/tmp/id_found")

	_, cmd := agent.addFoundKey()
	msg, ok := cmd().(daemonStartedForAddMsg)
	if !ok || msg.err != nil {
		t.Fatalf("cmd msg = %#v, want success", msg)
	}
	if st.starts != 0 {
		t.Errorf("daemon started %d time(s), want 0 when already running", st.starts)
	}
	_, _ = agent.Update(msg)
	if len(st.addedPaths) != 1 {
		t.Errorf("added paths = %v, want one add", st.addedPaths)
	}
}

func TestAgentFoundKeyDaemonStartFailure(t *testing.T) {
	st := installAddFlowStubs(t)
	st.startErr = errors.New("sshushd: binary not found")
	agent := newAgentWithFoundKey("/tmp/id_found")

	_, cmd := agent.addFoundKey()
	msg := cmd().(daemonStartedForAddMsg)
	if msg.err == nil {
		t.Fatal("expected start error in msg")
	}

	_, cmd = agent.Update(msg)
	if cmd == nil {
		t.Error("start failure should still re-check daemon state")
	}
	if !agent.statusErr || !strings.Contains(agent.status, "start failed") || !strings.Contains(agent.status, "binary not found") {
		t.Errorf("status=%q err=%v, want start failure error", agent.status, agent.statusErr)
	}
	if agent.daemonRunning {
		t.Error("daemonRunning should stay false after a failed start")
	}
	if len(st.addedPaths) != 0 {
		t.Errorf("key add attempted after failed start: %v", st.addedPaths)
	}
}

func TestAgentFileSelectedDaemonStoppedStartsThenAdds(t *testing.T) {
	st := installAddFlowStubs(t)
	_, agent := newAgentTestSkeleton()
	agent.daemonRunning = false

	_, cmd := agent.Update(FileSelectedMsg{Path: "/tmp/id_picked"})
	msg, ok := cmd().(daemonStartedForAddMsg)
	if !ok || msg.path != "/tmp/id_picked" {
		t.Fatalf("cmd msg = %#v, want daemonStartedForAddMsg for picked file", msg)
	}
	if st.starts != 1 {
		t.Fatalf("daemon started %d time(s), want 1", st.starts)
	}
	_, _ = agent.Update(msg)
	if len(st.addedPaths) != 1 || st.addedPaths[0] != "/tmp/id_picked" {
		t.Errorf("added paths = %v, want [/tmp/id_picked]", st.addedPaths)
	}
}

func TestAgentFileSelectedDaemonRunningAddsDirectly(t *testing.T) {
	st := installAddFlowStubs(t)
	_, agent := newAgentTestSkeleton()
	agent.daemonRunning = true

	_, cmd := agent.Update(FileSelectedMsg{Path: "/tmp/id_picked"})
	if got, ok := cmd().(stubAddedMsg); !ok || got.path != "/tmp/id_picked" {
		t.Fatalf("cmd msg = %#v, want direct add", got)
	}
	if st.starts != 0 {
		t.Errorf("daemon started %d time(s), want 0", st.starts)
	}
}
