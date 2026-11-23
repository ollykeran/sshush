//go:build !windows

package clipboard

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// With no override, each platform keeps the command it always had.
func TestDefaultCommand(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	if got, _ := defaultCommand("linux"); !reflect.DeepEqual(got, []string{"wl-copy"}) {
		t.Errorf("linux under Wayland: %q, want wl-copy", got)
	}
	t.Setenv("WAYLAND_DISPLAY", "")
	if got, _ := defaultCommand("linux"); !reflect.DeepEqual(got, []string{"xclip", "-selection", "clipboard"}) {
		t.Errorf("linux under X11: %q, want xclip -selection clipboard", got)
	}
	if got, _ := defaultCommand("darwin"); !reflect.DeepEqual(got, []string{"pbcopy"}) {
		t.Errorf("darwin: %q, want pbcopy", got)
	}
	_, err := defaultCommand("plan9")
	if err == nil || !strings.Contains(err.Error(), CopyCmdEnv) {
		t.Errorf("plan9: err = %v, want one pointing at %s", err, CopyCmdEnv)
	}
}

// A blank $SSHUSH_COPY_CMD is unset: Copy goes to the platform's default. With
// PATH emptied that default cannot be found, and the error names it rather
// than any override.
func TestCopy_blankOverrideFallsBackToTheDefault(t *testing.T) {
	t.Setenv(CopyCmdEnv, "   ")
	t.Setenv("PATH", "")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	argv, err := defaultCommand(runtime.GOOS)
	if err != nil {
		t.Skip(err)
	}
	err = Copy("text")
	if err == nil {
		t.Fatal("Copy succeeded with an empty PATH")
	}
	if !strings.Contains(err.Error(), argv[0]) {
		t.Errorf("error %q does not name the default command %q", err, argv[0])
	}
}
