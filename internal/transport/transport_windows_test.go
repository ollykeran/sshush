package transport

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	winio "github.com/Microsoft/go-winio"
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

func currentUserSID(t *testing.T) *windows.SID {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return user.User.Sid
}

func mustSID(t *testing.T, s string) *windows.SID {
	t.Helper()
	sid, err := windows.StringToSid(s)
	if err != nil {
		t.Fatalf("StringToSid(%q): %v", s, err)
	}
	return sid
}

// The decision itself, apart from any pipe: which owners a client will send
// secrets to. This is the part that runs whatever rights the test has.
func TestTrustedPipeOwner(t *testing.T) {
	user := mustSID(t, "S-1-5-21-1111111111-2222222222-3333333333-1001")
	tests := []struct {
		name  string
		owner *windows.SID
		want  bool
	}{
		{"the user", mustSID(t, user.String()), true},
		{"SYSTEM", mustSID(t, "S-1-5-18"), true},
		{"Administrators", mustSID(t, "S-1-5-32-544"), true},
		{"another user of the machine", mustSID(t, "S-1-5-21-1111111111-2222222222-3333333333-1002"), false},
		{"the same RID in another domain", mustSID(t, "S-1-5-21-4444444444-2222222222-3333333333-1001"), false},
		{"Everyone", mustSID(t, "S-1-1-0"), false},
		{"Users", mustSID(t, "S-1-5-32-545"), false},
		{"Guests", mustSID(t, "S-1-5-32-546"), false},
		{"LOCAL SERVICE", mustSID(t, "S-1-5-19"), false},
		{"no owner", nil, false},
	}
	for _, tt := range tests {
		if got := trustedPipeOwner(tt.owner, user); got != tt.want {
			t.Errorf("trustedPipeOwner(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}
	if trustedPipeOwner(user, nil) {
		t.Error("trustedPipeOwner with no current user = true")
	}
}

// A pipe this process serves is the ordinary case, the agent and its client
// being the same user, and Dial must go on accepting it. Under an elevated
// account (CI's is one) the owner Windows records is Administrators rather than
// the user, so this also covers that branch there.
func TestPipe_ownPipeIsTrusted(t *testing.T) {
	name := testPipe(t)
	l, err := Listen(name)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		if conn, err := l.Accept(); err == nil {
			_ = conn.Close()
		}
	}()

	conn, err := Dial(name)
	if err != nil {
		t.Fatalf("Dial of a pipe this process serves: %v", err)
	}
	defer conn.Close()
	if err := checkPipeOwner(conn, name); err != nil {
		t.Fatalf("checkPipeOwner: %v", err)
	}
}

// What the check is for: a pipe of the right name that somebody else owns.
// Without a second account to create one, the owner is set by hand to Guests,
// which Windows only allows a process holding SeRestorePrivilege, so this runs
// where the tests are elevated (CI) and is skipped where they are not. The ACL
// still lets this user in: being allowed to connect is not what is under test.
func TestPipe_dialRefusesForeignOwner(t *testing.T) {
	name := testPipe(t)
	sddl := "O:BGD:P(A;;GA;;;" + currentUserSID(t).String() + ")(A;;GA;;;SY)"

	var l net.Listener
	err := winio.RunWithPrivilege(winio.SeRestorePrivilege, func() error {
		var err error
		l, err = winio.ListenPipe(name, &winio.PipeConfig{SecurityDescriptor: sddl})
		return err
	})
	if err != nil {
		t.Skipf("cannot create a pipe owned by another account here: %v", err)
	}
	defer l.Close()
	go func() {
		if conn, err := l.Accept(); err == nil {
			_ = conn.Close()
		}
	}()

	conn, err := Dial(name)
	if err == nil {
		_ = conn.Close()
		t.Fatal("Dial connected to a pipe owned by Guests")
	}
	if !errors.Is(err, ErrForeignPipe) {
		t.Fatalf("Dial error = %v, want one wrapping ErrForeignPipe", err)
	}
	if !strings.Contains(err.Error(), "S-1-5-32-546") {
		t.Errorf("error %q does not name the owner", err)
	}
}
