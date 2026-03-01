package agent

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/openssh"
)

// DiscoverKeyPaths finds candidate private keys from config, ~/.ssh, and cwd.
func DiscoverKeyPaths(configPath string) []string {
	seen := make(map[string]bool)
	var paths []string

	addPath := func(p string) {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if seen[abs] {
			return
		}
		if _, err := os.Stat(abs); err != nil {
			return
		}
		seen[abs] = true
		paths = append(paths, abs)
	}

	if configPath != "" {
		cfg, err := config.LoadConfig(configPath)
		if err == nil {
			for _, p := range cfg.KeyPaths {
				addPath(p)
			}
		}
	}

	var searchDirs []string
	if home, err := os.UserHomeDir(); err == nil {
		searchDirs = append(searchDirs, filepath.Join(home, ".ssh"))
	}
	if cwd, err := os.Getwd(); err == nil {
		searchDirs = append(searchDirs, cwd)
	}

	for _, dir := range searchDirs {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() || strings.HasSuffix(e.Name(), ".pub") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			if seen[path] {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil || len(data) == 0 {
				continue
			}
			if _, err := openssh.ParsePrivateKeyBlob(data); err == nil {
				addPath(path)
			}
		}
	}

	return paths
}
