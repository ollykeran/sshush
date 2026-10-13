package style

import (
	"regexp"
	"strings"
	"testing"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestRelayedAgentModeIndicatorLine(t *testing.T) {
	cases := []struct {
		name          string
		liveMode      string
		reachable     bool
		skipNote      bool
		want, notWant string
	}{
		{"answered", "vault", true, false, "Agent: Windows vault (WSL relay)", "not reachable"},
		{"not answering", "", false, false, "Agent: Windows (WSL relay) (agent not reachable)", "vault"},
		{"not started yet", "", false, true, "Agent: Windows (WSL relay)", "not reachable"},
	}
	for _, c := range cases {
		got := ansiRE.ReplaceAllString(RelayedAgentModeIndicatorLine(c.liveMode, c.reachable, c.skipNote), "")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: want %q in %q", c.name, c.want, got)
		}
		if strings.Contains(got, c.notWant) {
			t.Errorf("%s: did not want %q in %q", c.name, c.notWant, got)
		}
	}
}
