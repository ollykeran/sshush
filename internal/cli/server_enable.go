package cli

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
)

// exampleListenPort is the setting suggested when the config has no listen_port
// line of its own to point at.
const exampleListenPort = "listen_port = 2222"

var (
	// tomlTableHeader matches a table header, commented out or not: "[server]",
	// "# [vault]".
	tomlTableHeader = regexp.MustCompile(`^\s*(#\s*)?\[\s*([^\[\]]+?)\s*\]\s*(#.*)?$`)
	// listenPortKey matches a listen_port assignment, commented out or not.
	listenPortKey = regexp.MustCompile(`^\s*(#\s*)?listen_port\s*=`)
)

// enableStep is one edit that would turn the server on: what to do, and the line
// of the config file to do it at. Line 0 means there is no line to point at.
type enableStep struct {
	line int
	edit string
}

// serverTableSpot is one [server] table found in a config file, commented out or
// not, with its listen_port line if it has one.
type serverTableSpot struct {
	header, port                   int // 1-based line numbers; port is 0 when there is none
	headerCommented, portCommented bool
	portText                       string // the listen_port line without its comment marker
}

// serverNotEnabled is what `sshush server` and `sshush server status` say when
// [server].listen_port is unset: a warning, and the lines of the config file at
// configPath to change. The server is never switched on for the user — it hands
// out a shell as them, so enabling it is left as an edit they make.
func serverNotEnabled(configPath string) *style.Output {
	out := style.NewOutput().Warn("SSH server is not enabled ([server].listen_port is unset or 0).")
	for _, line := range serverEnableHint(configPath) {
		out.Info(line)
	}
	return out.Info("Then run 'sshush server'.")
}

// serverEnableHint renders serverEnableSteps for the config at configPath as
// "path:line: edit" lines, the form editors and terminals open at the line.
func serverEnableHint(configPath string) []string {
	display := utils.DisplayPath(configPath)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return []string{fmt.Sprintf("%s: set %s under [server]", display, exampleListenPort)}
	}
	steps := serverEnableSteps(string(data))
	lines := make([]string, 0, len(steps))
	for _, step := range steps {
		if step.line == 0 {
			lines = append(lines, fmt.Sprintf("%s: %s", display, step.edit))
		} else {
			lines = append(lines, fmt.Sprintf("%s:%d: %s", display, step.line, step.edit))
		}
	}
	return lines
}

// serverEnableSteps works out the edits to a config file's content that would
// enable the server, pointing at lines already there wherever it can: the
// commented-out [server] block a generated config ships with, a listen_port left
// commented in a live table, or a listen_port = 0.
func serverEnableSteps(content string) []enableStep {
	spot, found := findServerTable(content)
	if !found {
		return []enableStep{{edit: "add a [server] table with " + exampleListenPort}}
	}
	var steps []enableStep
	if spot.headerCommented {
		steps = append(steps, enableStep{line: spot.header, edit: "uncomment [server]"})
	}
	switch {
	case spot.port == 0:
		steps = append(steps, enableStep{line: spot.header, edit: "add " + exampleListenPort + " under [server]"})
	case spot.portCommented:
		steps = append(steps, enableStep{line: spot.port, edit: "uncomment " + spot.portText})
	default:
		steps = append(steps, enableStep{line: spot.port, edit: "set a port above 0, e.g. " + exampleListenPort})
	}
	return steps
}

// findServerTable finds the [server] table to point at: the first live one, which
// is the one TOML reads, or else the first commented-out one. A listen_port only
// belongs to a table if it comes before the next header, commented out or not.
func findServerTable(content string) (serverTableSpot, bool) {
	var tables []serverTableSpot
	current := -1 // index in tables of the [server] table being read, or -1
	for i, line := range strings.Split(content, "\n") {
		n := i + 1
		if m := tomlTableHeader.FindStringSubmatch(line); m != nil {
			current = -1
			if m[2] == "server" {
				tables = append(tables, serverTableSpot{header: n, headerCommented: m[1] != ""})
				current = len(tables) - 1
			}
			continue
		}
		if current < 0 {
			continue
		}
		if m := listenPortKey.FindStringSubmatch(line); m != nil {
			t := &tables[current]
			commented := m[1] != ""
			// A live listen_port wins over a commented-out example in the same table.
			if t.port == 0 || (t.portCommented && !commented) {
				t.port, t.portCommented = n, commented
				t.portText = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
			}
		}
	}
	for _, t := range tables {
		if !t.headerCommented {
			return t, true
		}
	}
	if len(tables) > 0 {
		return tables[0], true
	}
	return serverTableSpot{}, false
}
