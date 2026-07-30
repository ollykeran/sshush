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

func VerifyBinaryChecksum() (string, error) {
	if Version == "dev" {
		return "", nil
	}

	binPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("version: get executable path: %w", err)
	}
	platformKey := platformKeyFor(binPath)

	url := fmt.Sprintf("https://github.com/ollykeran/sshush/releases/download/v%s/%s.sha256", Version, platformKey)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("version: fetch checksum: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("version: no checksum available for v%s/%s", Version, platformKey)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("version: fetch checksum: HTTP %d", resp.StatusCode)
	}

	return verifyChecksum(binPath, resp.Body)
}

func platformKeyFor(binPath string) string {
	return fmt.Sprintf("%s-%s-%s", filepath.Base(binPath), runtime.GOOS, runtime.GOARCH)
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
