package bridge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/transport"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// The tests stand this test binary in for sshush.exe: started with fakeEnv set
// it behaves as the Windows sshush does for the two commands the bridge runs,
// with its "agent" at the socket named by fakeAgentEnv.
const (
	fakeEnv        = "SSHUSH_BRIDGE_FAKE_SSHUSH"
	fakeAgentEnv   = "SSHUSH_BRIDGE_FAKE_AGENT"
	fakeStartedEnv = "SSHUSH_BRIDGE_FAKE_STARTED" // file "start" creates to show it ran
	fakeStartFails = "SSHUSH_BRIDGE_FAKE_START_FAILS"
)

func TestMain(m *testing.M) {
	if os.Getenv(fakeEnv) != "" {
		os.Exit(fakeSSHush(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeSSHush(args []string) int {
	if len(args) == 0 {
		return 2
	}
	switch args[0] {
	case "relay":
		conn, err := transport.Dial(os.Getenv(fakeAgentEnv))
		if err != nil {
			fmt.Fprintln(os.Stderr, "no agent is running at the fake pipe")
			return 1
		}
		defer conn.Close()
		Relay(conn, os.Stdin, os.Stdout)
		return 0
	case "start":
		if os.Getenv(fakeStartFails) != "" {
			fmt.Fprintln(os.Stdout, "$env:SSH_AUTH_SOCK = 'ignored'")
			fmt.Fprintln(os.Stderr, "[vault].vault_path is set but vault file not found")
			return 1
		}
		if err := os.WriteFile(os.Getenv(fakeStartedEnv), []byte("started"), 0o600); err != nil {
			return 1
		}
		return 0
	}
	return 2
}

// fakeWindows points the bridge at this test binary acting as sshush.exe.
func fakeWindows(t *testing.T, agentSocket string) *Windows {
	t.Helper()
	t.Setenv(fakeEnv, "1")
	t.Setenv(fakeAgentEnv, agentSocket)
	// Not os.Args[0]: that can be a bare file name, which is not something to
	// run from another working directory.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return &Windows{Path: self}
}

func socketDir(t *testing.T) string {
	t.Helper()
	base := "/tmp"
	if runtime.GOOS == "windows" {
		// %TEMP%, not t.TempDir(): a socket path has a short length limit.
		base = ""
	}
	dir, err := os.MkdirTemp(base, "sshush-bridge-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// serve runs handle for every connection to a fresh socket until the test ends.
func serve(t *testing.T, socketPath string, handle func(context.Context, net.Conn)) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
	})
	ready := make(chan struct{})
	go func() {
		_ = agent.ListenAndHandle(ctx, socketPath, func(conn net.Conn) { handle(ctx, conn) }, agent.WithReady(func() { close(ready) }))
	}()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatalf("listener on %s did not become ready", socketPath)
	}
}

// TestServe_carriesTheAgentProtocol is the whole feature in miniature: a client
// on one socket, an agent on another, and nothing between them but a relay
// process started for the connection.
func TestServe_carriesTheAgentProtocol(t *testing.T) {
	dir := socketDir(t)
	agentSocket := filepath.Join(dir, "agent.sock")
	keyring := sshagent.NewKeyring()
	serve(t, agentSocket, func(_ context.Context, conn net.Conn) {
		_ = sshagent.ServeAgent(keyring, conn)
	})
	windows := fakeWindows(t, agentSocket)
	bridgeSocket := filepath.Join(dir, "bridge.sock")
	serve(t, bridgeSocket, windows.Serve)

	session, err := agent.Open(bridgeSocket)
	if err != nil {
		t.Fatalf("open the bridge socket: %v", err)
	}
	defer session.Close()
	if keys, err := session.List(); err != nil || len(keys) != 0 {
		t.Fatalf("list through the bridge: want 0 keys, got %d (err %v)", len(keys), err)
	}
	// Lock and unlock change state in the agent, so they prove requests
	// arrive there and not at something answering in its place.
	if err := session.Lock([]byte("through-the-bridge")); err != nil {
		t.Fatalf("lock through the bridge: %v", err)
	}
	if err := keyring.Unlock([]byte("through-the-bridge")); err != nil {
		t.Fatalf("the agent behind the bridge was not locked by it: %v", err)
	}
	// Several requests on the one connection, then a second connection.
	for i := 0; i < 3; i++ {
		if _, err := session.List(); err != nil {
			t.Fatalf("list %d on the same connection: %v", i, err)
		}
	}
	second, err := agent.Open(bridgeSocket)
	if err != nil {
		t.Fatalf("open a second connection: %v", err)
	}
	defer second.Close()
	if _, err := second.List(); err != nil {
		t.Fatalf("list on a second connection: %v", err)
	}
}

// TestServe_nothingBehindTheRelayClosesTheConnection: with no agent on the
// other side the relay exits at once, and the client must be told so by the
// connection closing, not left waiting on it.
func TestServe_nothingBehindTheRelayClosesTheConnection(t *testing.T) {
	dir := socketDir(t)
	windows := fakeWindows(t, filepath.Join(dir, "no-agent-here.sock"))
	bridgeSocket := filepath.Join(dir, "bridge.sock")
	serve(t, bridgeSocket, windows.Serve)

	session, err := agent.Open(bridgeSocket)
	if err != nil {
		t.Fatalf("open the bridge socket: %v", err)
	}
	defer session.Close()
	failed := make(chan error, 1)
	go func() {
		_, err := session.List()
		failed <- err
	}()
	select {
	case err := <-failed:
		if err == nil {
			t.Fatal("list with nothing behind the relay: want an error, got nil")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("list with nothing behind the relay hung instead of failing")
	}
}

// TestServe_returnsWhenTheClientHangsUp: the relay process must not outlive
// the connection it was started for.
func TestServe_returnsWhenTheClientHangsUp(t *testing.T) {
	dir := socketDir(t)
	agentSocket := filepath.Join(dir, "agent.sock")
	keyring := sshagent.NewKeyring()
	serve(t, agentSocket, func(_ context.Context, conn net.Conn) {
		_ = sshagent.ServeAgent(keyring, conn)
	})
	windows := fakeWindows(t, agentSocket)

	client, server := net.Pipe()
	served := make(chan struct{})
	go func() {
		windows.Serve(context.Background(), server)
		close(served)
	}()
	agentClient := sshagent.NewClient(client)
	if _, err := agentClient.List(); err != nil {
		t.Fatalf("list: %v", err)
	}
	_ = client.Close()
	select {
	case <-served:
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after the client hung up")
	}
}

func TestStartDaemon_runsStart(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	windows := fakeWindows(t, "")
	t.Setenv(fakeStartedEnv, marker)

	if err := windows.StartDaemon(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("want 'start' to have run on the other side: %v", err)
	}
}

// TestStartDaemon_reportsWhatTheOtherSideSaid: what sshush.exe printed is the
// only account there is of a failure on Windows, so it has to reach the user.
// The line start prints for a Windows shell to evaluate must not.
func TestStartDaemon_reportsWhatTheOtherSideSaid(t *testing.T) {
	windows := fakeWindows(t, "")
	t.Setenv(fakeStartFails, "1")

	err := windows.StartDaemon(context.Background())
	if err == nil {
		t.Fatal("start: want an error, got nil")
	}
	if !strings.Contains(err.Error(), "vault file not found") {
		t.Fatalf("want the other side's reason in the error, got: %v", err)
	}
	if strings.Contains(err.Error(), "SSH_AUTH_SOCK") {
		t.Fatalf("want stdout kept out of the error, got: %v", err)
	}
}

func TestDiagnose_returnsWhatTheRelaySaid(t *testing.T) {
	windows := fakeWindows(t, filepath.Join(t.TempDir(), "no-agent-here.sock"))
	if said := windows.Diagnose(context.Background()); !strings.Contains(said, "no agent is running") {
		t.Fatalf("want the relay's own explanation, got %q", said)
	}
}

func TestFind_configuredPath(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, windowsBinary)
	if err := os.WriteFile(binary, []byte("not really"), 0o755); err != nil {
		t.Fatal(err)
	}

	windows, err := Find(binary)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if windows.Path != binary {
		t.Fatalf("path: want %s, got %s", binary, windows.Path)
	}

	if _, err := Find(filepath.Join(dir, "missing", windowsBinary)); err == nil || !strings.Contains(err.Error(), "windows_sshush") {
		t.Fatalf("a path that is not there: want an error naming the setting, got: %v", err)
	}
	if _, err := Find(dir); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("a directory: want an error saying so, got: %v", err)
	}
}

func TestFind_onPath(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, windowsBinary)
	if err := os.WriteFile(binary, []byte("not really"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir)
	windows, err := Find("")
	if err != nil {
		t.Fatalf("find on PATH: %v", err)
	}
	if windows.Path != binary {
		t.Fatalf("path: want %s, got %s", binary, windows.Path)
	}

	t.Setenv("PATH", t.TempDir())
	if _, err := Find(""); err == nil || !strings.Contains(err.Error(), "windows_sshush") {
		t.Fatalf("nothing on PATH: want an error naming the setting to use instead, got: %v", err)
	}
}

// TestRelay_endsWhenInputEnds: input ending means the client has gone, and the
// relay must not wait for an agent that has nothing further to say.
func TestRelay_endsWhenInputEnds(t *testing.T) {
	agentSide, far := net.Pipe()
	defer far.Close()
	go func() { _, _ = io.Copy(io.Discard, far) }() // an agent that reads and never answers

	done := make(chan struct{})
	go func() {
		Relay(agentSide, strings.NewReader("a request"), io.Discard)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Relay did not return after its input ended")
	}
}

// TestRelay_endsWhenTheAgentHangsUp, and passes on what it said first.
func TestRelay_endsWhenTheAgentHangsUp(t *testing.T) {
	agentSide, far := net.Pipe()
	go func() {
		_, _ = far.Write([]byte("an answer"))
		_ = far.Close()
	}()
	stdin, stdinWriter := io.Pipe() // input that never ends
	defer stdinWriter.Close()

	var out bytes.Buffer
	done := make(chan struct{})
	go func() {
		Relay(agentSide, stdin, &out)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Relay did not return after the agent hung up")
	}
	if out.String() != "an answer" {
		t.Fatalf("output: want %q, got %q", "an answer", out.String())
	}
}

// TestTidy_keepsTheWordsOfABoxedColouredError: an sshush.exe that does not know
// the relay command says so before it reads --no-color, so the reason arrives
// dressed for a terminal and has to be quotable in a sentence.
func TestTidy_keepsTheWordsOfABoxedColouredError(t *testing.T) {
	boxed := "\x1b[38;2;126;231;135m╭────────────────────────╮\x1b[m\r\n" +
		"\x1b[38;2;126;231;135m│\x1b[m \x1b[38;2;248;113;113munknown command \"relay\"\x1b[m \x1b[38;2;126;231;135m│\x1b[m\r\n" +
		"\x1b[38;2;126;231;135m│\x1b[m \x1b[38;2;248;113;113m\x1b[m                        \x1b[38;2;126;231;135m│\x1b[m\r\n" +
		"\x1b[38;2;126;231;135m│\x1b[m \x1b[38;2;248;113;113mDid you mean this?\x1b[m     \x1b[38;2;126;231;135m│\x1b[m\r\n" +
		"\x1b[38;2;126;231;135m╰────────────────────────╯\x1b[m\r\n"
	if got, want := tidy(boxed), `unknown command "relay"; Did you mean this?`; got != want {
		t.Fatalf("tidy: want %q, got %q", want, got)
	}
	if got, want := tidy("no agent is running at \\\\.\\pipe\\sshush-agent\r\nStart it with 'sshush start'.\r\n"), `no agent is running at \\.\pipe\sshush-agent; Start it with 'sshush start'.`; got != want {
		t.Fatalf("tidy of plain output: want %q, got %q", want, got)
	}
	if got := tidy(" \r\n\n"); got != "" {
		t.Fatalf("tidy of nothing: want empty, got %q", got)
	}
}
