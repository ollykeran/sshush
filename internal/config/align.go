package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/ollykeran/sshush/internal/platform"
)

// AlignAgentPipe points what tells ssh where the agent is at pipe, wherever it
// still names one of stale: SSH_AUTH_SOCK as new programs get it, and
// IdentityAgent lines in ~/.ssh/config. A value naming any other agent is the
// user's choice and is left alone, as is an unset one. It returns a phrase for
// each thing it changed, for a status line, and the errors it met.
func AlignAgentPipe(pipe string, stale ...string) ([]string, error) {
	pipe = strings.TrimSpace(pipe)
	isStale := func(v string) bool {
		if samePipe(v, pipe) {
			return false
		}
		for _, s := range stale {
			if strings.TrimSpace(s) != "" && samePipe(v, s) {
				return true
			}
		}
		return false
	}
	if pipe == "" {
		return nil, nil
	}
	var changed []string
	var errs []error
	if v, ok := platform.UserEnv("SSH_AUTH_SOCK"); ok && isStale(v) {
		if err := platform.SetUserEnv("SSH_AUTH_SOCK", pipe); err != nil {
			errs = append(errs, err)
		} else {
			changed = append(changed, "SSH_AUTH_SOCK for new windows")
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		path := filepath.Join(home, ".ssh", "config")
		ok, err := alignIdentityAgent(path, pipe, isStale)
		if err != nil {
			errs = append(errs, err)
		} else if ok {
			changed = append(changed, "IdentityAgent in "+filepath.Join("~", ".ssh", "config"))
		}
	}
	return changed, errors.Join(errs...)
}

// alignIdentityAgent rewrites the IdentityAgent lines in the ssh config at path
// whose agent isStale to name pipe, keeping everything else byte for byte. It
// reports whether it changed the file.
func alignIdentityAgent(path, pipe string, isStale func(string) bool) (bool, error) {
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	lines := strings.SplitAfter(string(content), "\n")
	changed := false
	for i, line := range lines {
		body := strings.TrimRight(line, "\r\n")
		ending := line[len(body):]
		trimmed := strings.TrimSpace(body)
		fields := strings.Fields(trimmed)
		if len(fields) != 2 || !strings.EqualFold(fields[0], "identityagent") {
			continue
		}
		if !isStale(strings.Trim(fields[1], `"`)) {
			continue
		}
		indent := body[:len(body)-len(strings.TrimLeft(body, " \t"))]
		// Forward slashes: ssh treats a backslash in its config as an escape.
		lines[i] = indent + fields[0] + " " + strings.ReplaceAll(pipe, `\`, "/") + ending
		changed = true
	}
	if !changed {
		return false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(strings.Join(lines, "")), info.Mode().Perm())
}
