package clipboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The test binary doubles as the copy command: run with helperEnv set, it
// records the arguments it was given and what came in on stdin to the file
// helperEnv names, and exits. With helperFailEnv set as well it fails instead,
// as a copy command with no clipboard to reach would.
const (
	helperEnv     = "SSHUSH_CLIPBOARD_TEST_HELPER_OUT"
	helperFailEnv = "SSHUSH_CLIPBOARD_TEST_HELPER_FAIL"
)

type helperRecord struct {
	Args  []string
	Stdin string
}

func TestMain(m *testing.M) {
	if out := os.Getenv(helperEnv); out != "" {
		os.Exit(runHelper(out))
	}
	os.Exit(m.Run())
}

func runHelper(out string) int {
	if os.Getenv(helperFailEnv) != "" {
		fmt.Fprintln(os.Stderr, "helper: no clipboard here")
		return 3
	}
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	data, err := json.Marshal(helperRecord{Args: os.Args[1:], Stdin: string(stdin)})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(out, data, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// helperCommand returns an $SSHUSH_COPY_CMD value that runs the test binary
// as the helper with args, and the file it will record to.
func helperCommand(t *testing.T, args string) (command, out string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(exe, `"`) {
		t.Skipf("test binary path %q cannot be double-quoted", exe)
	}
	out = filepath.Join(t.TempDir(), "record.json")
	t.Setenv(helperEnv, out)
	command = `"` + exe + `"`
	if args != "" {
		command += " " + args
	}
	return command, out
}

func readRecord(t *testing.T, out string) helperRecord {
	t.Helper()
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("the copy command did not run: %v", err)
	}
	var rec helperRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

// With $SSHUSH_COPY_CMD set, the text goes to that command's stdin, with the
// arguments the variable gave it, and the platform's own way is not used.
func TestCopy_runsTheCommandInTheEnvironmentWithTheTextOnStdin(t *testing.T) {
	command, out := helperCommand(t, `--clipboard 'two words' "-"`)
	t.Setenv(CopyCmdEnv, command)

	const text = "ssh-ed25519 AAAAC3Nza naïve café\n"
	if err := Copy(text); err != nil {
		t.Fatal(err)
	}
	rec := readRecord(t, out)
	if rec.Stdin != text {
		t.Errorf("stdin = %q, want %q", rec.Stdin, text)
	}
	if want := []string{"--clipboard", "two words", "-"}; !reflect.DeepEqual(rec.Args, want) {
		t.Errorf("args = %q, want %q", rec.Args, want)
	}
}

// The override is for wherever the default cannot reach the clipboard, and a
// recovery phrase has to get there too.
func TestCopySecret_runsTheCommandInTheEnvironment(t *testing.T) {
	command, out := helperCommand(t, "")
	t.Setenv(CopyCmdEnv, command)

	const phrase = "abandon ability able about"
	if err := CopySecret(phrase); err != nil {
		t.Fatal(err)
	}
	if rec := readRecord(t, out); rec.Stdin != phrase {
		t.Errorf("stdin = %q, want %q", rec.Stdin, phrase)
	}
}

// A command that fails is named in the error, so the user can tell it was
// theirs and not sshush that went wrong.
func TestCopy_namesTheCommandWhenItFails(t *testing.T) {
	command, _ := helperCommand(t, "")
	t.Setenv(helperFailEnv, "1")
	t.Setenv(CopyCmdEnv, command)

	err := Copy("text")
	if err == nil {
		t.Fatal("Copy succeeded with a failing command")
	}
	exe, _ := os.Executable()
	if !strings.Contains(err.Error(), exe) {
		t.Errorf("error %q does not name the command %q", err, exe)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Errorf("error %v does not carry the command's exit status 3", err)
	}
}

func TestCopy_namesACommandThatIsNotThere(t *testing.T) {
	t.Setenv(CopyCmdEnv, "sshush-no-such-copy-command --flag")
	err := Copy("text")
	if err == nil {
		t.Fatal("Copy succeeded with a missing command")
	}
	if !strings.Contains(err.Error(), "sshush-no-such-copy-command") {
		t.Errorf("error %q does not name the command", err)
	}
}

func TestCopy_rejectsAnUnterminatedQuote(t *testing.T) {
	t.Setenv(CopyCmdEnv, `xsel "--clipboard`)
	err := Copy("text")
	if err == nil || !strings.Contains(err.Error(), CopyCmdEnv) {
		t.Fatalf("err = %v, want an error naming %s", err, CopyCmdEnv)
	}
}

func TestOverrideCommand_unsetOrBlankMeansNoOverride(t *testing.T) {
	for _, value := range []string{"", " ", "\t \n"} {
		t.Setenv(CopyCmdEnv, value)
		argv, err := overrideCommand()
		if err != nil || argv != nil {
			t.Errorf("%s=%q: got %q, %v; want no override", CopyCmdEnv, value, argv, err)
		}
	}
	os.Unsetenv(CopyCmdEnv) // t.Setenv above restores it after the test
	if argv, err := overrideCommand(); err != nil || argv != nil {
		t.Errorf("unset: got %q, %v; want no override", argv, err)
	}
}

func TestSplitCommand(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"wl-copy", []string{"wl-copy"}},
		{"  xsel --clipboard --input  ", []string{"xsel", "--clipboard", "--input"}},
		{"tmux load-buffer -", []string{"tmux", "load-buffer", "-"}},
		{"clip.exe", []string{"clip.exe"}},
		{"xclip\t-selection\tclipboard", []string{"xclip", "-selection", "clipboard"}},
		{`"/opt/My Tools/copy" --in`, []string{"/opt/My Tools/copy", "--in"}},
		{`copy 'it''s' "a b"c`, []string{"copy", "its", "a bc"}},
		{`copy ""`, []string{"copy", ""}},
		{`"C:\Program Files\Tool\copy.exe" /u`, []string{`C:\Program Files\Tool\copy.exe`, "/u"}},
		{`copy $HOME *.txt | tee`, []string{"copy", "$HOME", "*.txt", "|", "tee"}},
	}
	for _, tt := range tests {
		got, err := splitCommand(tt.in)
		if err != nil {
			t.Errorf("splitCommand(%q): %v", tt.in, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("splitCommand(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSplitCommand_errors(t *testing.T) {
	for _, in := range []string{"", "   ", `"unterminated`, `copy 'open`, `""`} {
		if got, err := splitCommand(in); err == nil {
			t.Errorf("splitCommand(%q) = %q, want an error", in, got)
		}
	}
}

// A program whose path has spaces in it can be given unquoted, as with $EDITOR.
func TestSplitCommand_takesAnExistingPathWithSpacesWhole(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "with space")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, filepath.Base(exe))
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := splitCommand(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{path}; !reflect.DeepEqual(got, want) {
		t.Errorf("splitCommand(%q) = %q, want %q", path, got, want)
	}
}
