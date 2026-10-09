package transport

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func testPipe(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf(`\\.\pipe\sshush-test-%d-%s`, os.Getpid(), strings.NewReplacer("/", "-", " ", "-").Replace(t.Name()))
}

func TestPipePath(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{`\\.\pipe\sshush-agent`, `\\.\pipe\sshush-agent`, true},
		{`//./pipe/sshush-agent`, `\\.\pipe\sshush-agent`, true},
		{`\\.\PIPE\openssh-ssh-agent`, `\\.\pipe\openssh-ssh-agent`, true},
		{`\\.\pipe\`, "", false},
		{`C:\Users\me\sshush.sock`, "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := pipePath(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("pipePath(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestPipe_roundTrip(t *testing.T) {
	echoOnce(t, testPipe(t))
}

func TestPipe_absLeavesNameAlone(t *testing.T) {
	got, err := Abs(`//./pipe/sshush-agent`)
	if err != nil || got != `\\.\pipe\sshush-agent` {
		t.Fatalf("Abs = %q, %v", got, err)
	}
}

func TestPipe_dialNothingListening(t *testing.T) {
	if _, err := Dial(testPipe(t)); err == nil {
		t.Fatal("expected an error dialling a pipe nobody serves")
	}
}

// A second listener must not be able to share (or squat on) a served pipe name.
func TestPipe_secondListenerRefused(t *testing.T) {
	name := testPipe(t)
	l, err := Listen(name)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if l2, err := Listen(name); err == nil {
		_ = l2.Close()
		t.Fatal("expected the second Listen on the same pipe to fail")
	}
}

// The ACL is the only thing between the agent's keys and other users of the
// machine, so check what was asked for: this user and SYSTEM, nobody else.
func TestOwnerOnlySDDL(t *testing.T) {
	sddl, err := ownerOnlySDDL()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatalf("SDDL %q does not parse: %v", sddl, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		t.Fatalf("no DACL in %q: %v", sddl, err)
	}
	if dacl.AceCount != 2 {
		t.Fatalf("got %d ACEs in %q, want 2 (user and SYSTEM)", dacl.AceCount, sddl)
	}
	for _, everyone := range []string{"WD", "AN", "BU", "AU"} {
		if strings.Contains(sddl, ";;;"+everyone+")") {
			t.Fatalf("SDDL %q grants access to %s", sddl, everyone)
		}
	}
}
