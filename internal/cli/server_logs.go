package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ollykeran/sshush/internal/platform"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
)

// followInterval is how often `sshush server logs -f` looks for new lines.
const followInterval = 250 * time.Millisecond

func newServerLogsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show the SSH server's log",
		Long: "Print the end of the SSH server's log: its startup, every connection and sign-in attempt, sessions, " +
			"and what it refused, one key=value record per line. The log is [server].log_file, or its default. " +
			"With --follow, keep printing lines as they are logged until interrupted.",
		Args: argsNoneOrHelp,
		RunE: runServerLogs,
	}
	cmd.Flags().BoolP("follow", "f", false, "keep printing new lines as they are logged")
	cmd.Flags().IntP("lines", "n", 50, "lines to show from the end of the log; 0 shows all of it")
	return cmd
}

func runServerLogs(cmd *cobra.Command, _ []string) error {
	if env.Config == nil {
		return style.NewOutput().Error("config not loaded").AsError()
	}
	path := platform.ServerLogPath(env.Config.ServerLogFile)
	lines, _ := cmd.Flags().GetInt("lines")
	follow, _ := cmd.Flags().GetBool("follow")

	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist) && !follow:
		return style.NewOutput().
			Warn("No server log at " + utils.DisplayPath(path) + " yet.").
			Info("It is created when 'sshush server' starts.").
			AsError()
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return style.NewOutput().Error("read server log: " + err.Error()).AsError()
	}

	out := cmd.OutOrStdout()
	if _, err := io.WriteString(out, tailLines(string(data), lines)); err != nil {
		return err
	}
	if !follow {
		return nil
	}
	parent := cmd.Context()
	if parent == nil {
		parent = context.Background()
	}
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return followLog(ctx, path, int64(len(data)), out, followInterval)
}

// tailLines returns the last n lines of text, or all of it when n is zero or less.
func tailLines(text string, n int) string {
	if n <= 0 {
		return text
	}
	end := len(strings.TrimSuffix(text, "\n"))
	for i := 0; i < n; i++ {
		newline := strings.LastIndexByte(text[:end], '\n')
		if newline < 0 {
			return text
		}
		end = newline
	}
	return text[end+1:]
}

// followLog writes whatever is appended to the log at path, from offset on, until
// ctx ends, looking every interval. A log that has been rotated — replaced by a new
// file — or truncated is read again from its start. Lines written to the old file
// in the moment before a rotation can be missed.
func followLog(ctx context.Context, path string, offset int64, w io.Writer, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	current, _ := os.Stat(path)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		info, err := os.Stat(path)
		if err != nil {
			continue // not created yet, or mid-rotation
		}
		if current == nil || !os.SameFile(current, info) || info.Size() < offset {
			offset = 0
		}
		current = info
		if info.Size() == offset {
			continue
		}
		n, err := copyLogFrom(path, offset, w)
		offset += n
		if err != nil {
			return err
		}
	}
}

// copyLogFrom writes the log at path from offset to its current end.
func copyLogFrom(path string, offset int64, w io.Writer) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil // rotated away between the stat and the open; next tick finds it
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	return io.Copy(w, f)
}
