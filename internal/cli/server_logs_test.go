package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTailLines(t *testing.T) {
	cases := []struct {
		text string
		n    int
		want string
	}{
		{"a\nb\nc\n", 2, "b\nc\n"},
		{"a\nb\nc\n", 3, "a\nb\nc\n"},
		{"a\nb\nc\n", 10, "a\nb\nc\n"},
		{"a\nb\nc", 1, "c"},
		{"a\nb\nc\n", 0, "a\nb\nc\n"},
		{"", 5, ""},
	}
	for _, tc := range cases {
		if got := tailLines(tc.text, tc.n); got != tc.want {
			t.Errorf("tailLines(%q, %d) = %q, want %q", tc.text, tc.n, got, tc.want)
		}
	}
}

// followBuffer collects what followLog writes, for the test to read meanwhile.
type followBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (f *followBuffer) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.b.Write(p)
}

func (f *followBuffer) String() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.b.String()
}

func waitForFollowed(t *testing.T, out *followBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("followed output never contained %q; got %q", want, out.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestFollowLog_PrintsNewLinesAndFollowsARotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	if err := os.WriteFile(path, []byte("already shown\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	out := &followBuffer{}
	done := make(chan error, 1)
	go func() { done <- followLog(ctx, path, int64(len("already shown\n")), out, 5*time.Millisecond) }()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("appended\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	waitForFollowed(t, out, "appended\n")

	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after rotation\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForFollowed(t, out, "after rotation\n")

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("followLog = %v, want nil once cancelled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("followLog kept going after its context ended")
	}
	if strings.Contains(out.String(), "already shown") {
		t.Errorf("followed output repeats lines from before the offset: %q", out.String())
	}
}
