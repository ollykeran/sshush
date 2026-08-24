package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2E_StartSurfacesRealDaemonError verifies that when the forked sshushd
// child fails after the sshush CLI's own preflight checks pass (a corrupt
// vault file, discovered only once the daemon opens it), `sshush start`
// reports the daemon's actual error instead of a generic "not ready" timeout.
func TestE2E_StartSurfacesRealDaemonError(t *testing.T) {
	dir := e2eWorkDir(t)
	socketPath := filepath.Join(dir, "agent.sock")
	vaultPath := filepath.Join(dir, "vault.json")

	binDir := buildBins(t)
	configPath := writeE2EConfig(t, dir, socketPath, vaultPath, nil)

	// A vault file that exists (so the CLI's own preflight "vault file not
	// found" check doesn't trip) but is not valid JSON, so vault.Open fails
	// only once sshushd actually tries to open it.
	if err := os.WriteFile(vaultPath, []byte("not valid json"), 0o600); err != nil {
		t.Fatalf("write corrupt vault: %v", err)
	}

	stdout, stderr, code := runSSHush(t, binDir, configPath, dir, nil, "start")
	if code == 0 {
		t.Fatalf("expected non-zero exit; stdout: %q stderr: %q", stdout, stderr)
	}
	combined := stdout + stderr
	if strings.Contains(combined, "not ready") {
		t.Errorf("expected the real vault error, not a readiness timeout; got: %q", combined)
	}
	if !strings.Contains(combined, "vault: parse") {
		t.Errorf("expected output to contain the daemon's real error (\"vault: parse ...\"); got: %q", combined)
	}
}
