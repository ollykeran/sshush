package utils

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ollykeran/sshush/internal/openssh"
)

// ExpandHomeDirectory replaces ~ with the current user's home directory.
func ExpandHomeDirectory(path string) string {
	if strings.Contains(path, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return strings.ReplaceAll(path, "~", homeDir)
	}
	return path
}

// ContractHomeDirectory replaces the current user's home directory prefix with ~.
func ContractHomeDirectory(path string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == homeDir {
		return "~"
	}
	if homeDir != "/" && strings.HasPrefix(path, homeDir+string(filepath.Separator)) {
		return "~" + string(filepath.Separator) + path[len(homeDir)+1:]
	}
	return path
}

// DiscoverKeyPaths finds valid private key files in searchDirs.
// If cwd is true, adds current directory. If ssh is true, adds ~/.ssh.
// If recursive is true, walks subdirectories.
func DiscoverKeyPaths(searchDirs []string, cwd bool, ssh bool, recursive bool) []string {
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

	// if configPath != "" {
	// 	cfg, err := config.LoadConfig(configPath)
	// 	if err == nil {
	// 		for _, p := range cfg.KeyPaths {
	// 			addPath(p)
	// 		}
	// 	}
	// }

	if ssh {
		if home, err := os.UserHomeDir(); err == nil {
			searchDirs = append(searchDirs, filepath.Join(home, ".ssh"))
		}
	}
	if cwd {
		if cwd, err := os.Getwd(); err == nil {
			searchDirs = append(searchDirs, cwd)
		}
	}

	tryAddKey := func(path string) {
		if seen[path] {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			return
		}
		if _, err := openssh.ParsePrivateKeyBlob(data); err == nil {
			addPath(path)
		}
	}

	for _, dir := range searchDirs {
		if recursive {
			filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if d.IsDir() || strings.HasSuffix(d.Name(), ".pub") {
					return nil
				}
				tryAddKey(path)
				return nil
			})
		} else {
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if e.IsDir() || strings.HasSuffix(e.Name(), ".pub") {
					continue
				}
				tryAddKey(filepath.Join(dir, e.Name()))
			}
		}
	}

	return paths
}
