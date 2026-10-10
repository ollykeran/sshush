package bridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/ollykeran/sshush/internal/utils"
)

// windowsBinary is the sshush CLI's file name on Windows.
const windowsBinary = "sshush.exe"

// startTimeout bounds "sshush.exe start". It covers starting a Windows process
// from WSL and that process starting its daemon, which is quick once warm and
// can take seconds the first time an antivirus sees a new binary.
const startTimeout = 30 * time.Second

// Windows is the sshush installed on the Windows side of this WSL distro.
type Windows struct {
	// Path is sshush.exe as this side sees it, e.g. /mnt/c/Tools/sshush/sshush.exe.
	Path string
}

// Find locates sshush.exe. configured is [agent].windows_sshush; when it is
// empty the binary is looked up on PATH, which under WSL includes the Windows
// PATH unless that has been turned off.
func Find(configured string) (*Windows, error) {
	if configured != "" {
		path := utils.ExpandHomeDirectory(configured)
		if fi, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("[agent].windows_sshush: %w", err)
		} else if fi.IsDir() {
			return nil, fmt.Errorf("[agent].windows_sshush: %s is a directory; name %s itself", utils.DisplayPath(path), windowsBinary)
		}
		return &Windows{Path: path}, nil
	}
	path, err := exec.LookPath(windowsBinary)
	if err != nil {
		return nil, errors.New("cannot find " + windowsBinary + " on PATH: put sshush on the Windows PATH, or set [agent].windows_sshush to where it is as WSL sees it (/mnt/c/...)")
	}
	return &Windows{Path: path}, nil
}

// command prepares sshush.exe with args. It runs from the binary's own
// directory: a Windows process started from a Linux directory gets a
// \\wsl.localhost\... path as its working directory, which some of Windows
// refuses, and this one is known to be a Windows directory.
func (w *Windows) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, w.Path, args...)
	cmd.Dir = filepath.Dir(w.Path)
	return cmd
}

// Serve relays one agent connection to the Windows agent, and returns when
// either end is done. It closes conn.
//
// A daemon that is not running on Windows makes the relay exit at once, so the
// client sees the connection close, the same as it would for a dead socket.
func (w *Windows) Serve(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	cmd := w.command(ctx, "relay")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	go func() {
		// The client hanging up closes the relay's stdin, which is what tells
		// the Windows process to exit.
		_, _ = io.Copy(stdin, conn)
		_ = stdin.Close()
	}()
	_, _ = io.Copy(conn, stdout)
	// The relay has nothing more to say, so stop reading from the client too.
	_ = conn.Close()
	_ = cmd.Wait()
}

// StartDaemon runs "sshush.exe start", which starts the Windows daemon if it is
// not running and is harmless if it is. A vault starts locked: start is given
// no input, so it skips its passphrase prompt, and the unlock is left to the
// caller, through the relay.
//
// On failure the error carries what sshush.exe printed, which is the only
// account of what went wrong on the other side.
func (w *Windows) StartDaemon(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	var output bytes.Buffer
	cmd := w.command(ctx, "start", "--no-color")
	cmd.Stdout = io.Discard // the SSH_AUTH_SOCK line, meant for a Windows shell
	cmd.Stderr = &output
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s start did not finish within %s", utils.DisplayPath(w.Path), startTimeout)
	}
	if said := tidy(output.String()); said != "" {
		return fmt.Errorf("%s start failed: %s", utils.DisplayPath(w.Path), said)
	}
	return fmt.Errorf("run %s start: %w", utils.DisplayPath(w.Path), err)
}

// Diagnose runs one relay with nothing to say and returns what it printed, for
// explaining why the Windows agent is not answering: an sshush.exe too old to
// know the relay command says so here, as does one that finds no agent.
func (w *Windows) Diagnose(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	var output bytes.Buffer
	cmd := w.command(ctx, "relay", "--no-color")
	cmd.Stdout = io.Discard
	cmd.Stderr = &output
	_ = cmd.Run()
	return tidy(output.String())
}

// tidy reduces what sshush.exe printed to the words in it, on one line, so it
// can be quoted inside this side's own message. --no-color covers most of
// that, but not an error raised before flags are read, such as an unknown
// command, which arrives coloured and boxed.
func tidy(output string) string {
	var lines []string
	for _, line := range strings.Split(ansi.Strip(output), "\n") {
		if line = strings.Trim(line, " \t\r│╭╮╰╯─+|-"); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "; ")
}
