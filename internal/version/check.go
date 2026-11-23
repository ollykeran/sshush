package version

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

var LatestReleaseURL = "https://api.github.com/repos/ollykeran/sshush/releases/latest"

type githubRelease struct {
	TagName string `json:"tag_name"`
}

func CheckLatest() (string, error) {
	if Version == "dev" {
		return "", nil
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", LatestReleaseURL, nil)
	if err != nil {
		return "", fmt.Errorf("version: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("version: fetch latest: %w", err)
	}
	defer resp.Body.Close()

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", fmt.Errorf("version: decode response: %w", err)
	}

	latest := strings.TrimPrefix(rel.TagName, "v")
	if compareSemver(Version, latest) < 0 {
		return fmt.Sprintf(
			"New version available: %s (current: %s) — run `go install github.com/ollykeran/sshush/cmd/sshush@latest` to upgrade",
			rel.TagName, Version,
		), nil
	}
	return "", nil
}

func compareSemver(a, b string) int {
	a = strings.SplitN(a, "-", 2)[0]
	b = strings.SplitN(b, "-", 2)[0]

	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}

	for i := 0; i < maxLen; i++ {
		var av, bv int
		if i < len(aParts) {
			av, _ = strconv.Atoi(aParts[i])
		}
		if i < len(bParts) {
			bv, _ = strconv.Atoi(bParts[i])
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

// ChecksumResult is what VerifyBinaryChecksum found. Verified is true when a check
// ran and passed; Message says what was checked, or why nothing was.
type ChecksumResult struct {
	Verified bool
	Message  string
}

// SumDBLookupURL is the Go checksum database's lookup endpoint; a module path and
// version, as path@version, are appended to it.
var SumDBLookupURL = "https://sum.golang.org/lookup/"

// VerifyBinaryChecksum checks the running binary against what was published for
// its version.
//
// A release build is compared byte for byte with the SHA-256 on the GitHub
// release. A go install build cannot be: the go command compiled it locally, with
// different settings, so its bytes never match. Instead the module source hash Go
// embedded in it is compared with the Go checksum database, which the go command
// checks downloaded modules against by default. That proves the binary was built
// from the published source for its version, not that its bytes are untouched. A
// development build, or one built from a checkout, is not checked.
func VerifyBinaryChecksum() (ChecksumResult, error) {
	switch {
	case Version == "dev":
		return ChecksumResult{Message: "not checked: development build"}, nil
	case fromBuildInfo && module.Sum == "":
		return ChecksumResult{Message: "not checked: built from source, not a release"}, nil
	case fromBuildInfo:
		return verifyModuleSum(module)
	}

	binPath, err := os.Executable()
	if err != nil {
		return ChecksumResult{}, fmt.Errorf("version: get executable path: %w", err)
	}
	platformKey := platformKeyFor(binPath)

	url := fmt.Sprintf("https://github.com/ollykeran/sshush/releases/download/v%s/%s.sha256", Version, platformKey)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return ChecksumResult{}, fmt.Errorf("version: fetch checksum: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ChecksumResult{}, fmt.Errorf("version: no checksum available for v%s/%s", Version, platformKey)
	}
	if resp.StatusCode != http.StatusOK {
		return ChecksumResult{}, fmt.Errorf("version: fetch checksum: HTTP %d", resp.StatusCode)
	}

	if _, err := verifyChecksum(binPath, resp.Body); err != nil {
		return ChecksumResult{}, err
	}
	return ChecksumResult{Verified: true, Message: "binary checksum verified"}, nil
}

// verifyModuleSum looks m up in the Go checksum database and compares the h1:
// source hash recorded there with the one embedded in the binary.
func verifyModuleSum(m debug.Module) (ChecksumResult, error) {
	url := SumDBLookupURL + escapeModulePath(m.Path) + "@" + m.Version
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return ChecksumResult{}, fmt.Errorf("version: look up %s@%s: %w", m.Path, m.Version, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ChecksumResult{}, fmt.Errorf("version: look up %s@%s: HTTP %d", m.Path, m.Version, resp.StatusCode)
	}

	recorded, err := moduleSumFromLookup(resp.Body, m.Path, m.Version)
	if err != nil {
		return ChecksumResult{}, err
	}
	if recorded != m.Sum {
		return ChecksumResult{}, fmt.Errorf("version: module source for %s does not match sum.golang.org", m.Version)
	}
	return ChecksumResult{Verified: true, Message: "built with go install: source matches sum.golang.org"}, nil
}

// moduleSumFromLookup finds the source hash for path@version in a checksum
// database lookup response, whose record lines read "path version h1:hash". The
// go.mod line, "path version/go.mod h1:hash", is a different record and is skipped.
func moduleSumFromLookup(r io.Reader, path, version string) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, 1<<16))
	if err != nil {
		return "", fmt.Errorf("version: read checksum database lookup: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[0] == path && f[1] == version && strings.HasPrefix(f[2], "h1:") {
			return f[2], nil
		}
	}
	return "", fmt.Errorf("version: no checksum database record for %s@%s", path, version)
}

// escapeModulePath applies the go command's case encoding for module paths in
// URLs: each upper-case letter becomes "!" followed by its lower-case form.
func escapeModulePath(path string) string {
	var b strings.Builder
	for _, r := range path {
		if 'A' <= r && r <= 'Z' {
			b.WriteByte('!')
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// platformKeyFor names the release checksum for the binary at binPath:
// "sshush-linux-amd64", and likewise "sshush-windows-amd64" for sshush.exe.
func platformKeyFor(binPath string) string {
	name := strings.TrimSuffix(filepath.Base(binPath), ".exe")
	return fmt.Sprintf("%s-%s-%s", name, runtime.GOOS, runtime.GOARCH)
}

func verifyChecksum(binPath string, r io.Reader) (string, error) {
	localHash, err := fileSHA256(binPath)
	if err != nil {
		return "", fmt.Errorf("version: compute hash: %w", err)
	}

	remoteHash, err := readHash(r)
	if err != nil {
		return "", fmt.Errorf("version: read checksum: %w", err)
	}

	if localHash == remoteHash {
		return "", nil
	}
	platformKey := platformKeyFor(binPath)
	return "", fmt.Errorf("version: checksum mismatch for %s", platformKey)
}

func readHash(r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
